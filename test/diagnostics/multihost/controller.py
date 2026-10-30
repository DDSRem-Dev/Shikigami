#!/usr/bin/env python3
"""Opt-in two-host acceptance. --check validates input without starting helpers.

Each command starts the JSON-lines helper as the device's ordinary OS user.
SSH is only a controller/opaque TLS carrier; never forward an agent Unix socket.
The operator provisions private configs, matched binaries and dedicated tokens.
This runner never manages accounts, installs services, authenticates tsnet, or
deletes a runtime tree. Root-owned provisioning/cleanup remains separate.
"""
import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import queue
import subprocess
import threading
import time
import uuid


class Failure(RuntimeError):
    pass


class OperationFailure(Failure):
    def __init__(self, device, operation, code):
        super().__init__(f'{device} {operation} failed: {code}')
        self.code = code


RPC_METHODS = frozenset('GetStatus GetPreferences SetPreferences ListServers ListMappings PutMapping RemoveMapping '
                        'GetSyncState ListConflicts ResolveConflict CreateForward StopForward ListForwards ListPlugins '
                        'ManagePlugin GetSystemInfo ListProjects CreateProject GetProject DeleteProject CreateSession '
                        'GetSession GetSessionScreen TakeControl DeleteSession StatFile DeleteFile'.split())
RPC_SERVICES = frozenset(('agent', 'remote', 'file-direct'))


def operation_context(op, args):
    """Only fixed protocol names enter diagnostics, never request payloads."""
    if op not in ('rpc', 'upload'):
        return {}, op
    method = args.get('method') if op == 'rpc' else 'WriteFile'
    service = args.get('service')
    safe = {'method': method if isinstance(method, str) and (method in RPC_METHODS or op == 'upload') else 'unrecognized',
            'service': service if isinstance(service, str) and service in RPC_SERVICES else 'unrecognized'}
    return safe, f"{op} {safe['service']}.{safe['method']}"


def require(condition, description):
    if not condition:
        raise Failure(description)


def request_id():
    return str(uuid.uuid4())


def digest(content):
    return hashlib.sha256(content.encode() if isinstance(content, str) else content).hexdigest()


def payload(pattern, size):
    data = pattern.encode()
    return (data * ((size + len(data) - 1) // len(data)))[:size]


def contained(root, path):
    root, path = PurePosixPath(root), PurePosixPath(path)
    return path.is_absolute() and '..' not in path.parts and path != root and root in path.parents


def validate(env):
    require(env.get('format') == 1, 'environment format must be 1')
    require(isinstance(env.get('run_id'), str) and env['run_id'], 'run_id required')
    require(isinstance(env.get('expected_version'), str) and env['expected_version'], 'expected_version required')
    require(Path(env.get('evidence_dir', '')).is_absolute(), 'absolute evidence_dir required')
    for name in ('a', 'b'):
        device = env[name]
        require(PurePosixPath(device['root']).is_absolute() and len(device['root']) <= 45,
                'short absolute device roots required for Unix socket limits')
        for key in ('files', 'token_file', 'ca_file'):
            require(contained(device['root'], device[key]), f'{name}.{key} must be within its private root')
        command = device['command']
        require(isinstance(command, list) and command and all(isinstance(s, str) for s in command), 'command must be an argv array')
        require(not any('password' in s.lower() or 'sshpass' in s.lower() for s in command), 'password-bearing commands forbidden')
        require(device['tls_target'].startswith('127.0.0.1:'), 'TLS target must be an operator-provisioned loopback endpoint')
    require(contained(env['b']['root'], env['daemon']['project_root']), 'daemon project must be private on B')
    require(env['b']['files'] != env['daemon']['project_root'], 'B local and daemon roots must differ')
    require(all(env['daemon'].get(k) for k in ('token_id_a', 'token_id_b')), 'dedicated token IDs required')
    require(env['daemon']['token_id_a'] != env['daemon']['token_id_b'], 'devices must have distinct credentials')


class Evidence:
    def __init__(self, directory):
        self.root = Path(directory)
        self.root.mkdir(mode=0o700, parents=True, exist_ok=False)
        self.lock = threading.Lock()
        self.events = []

    def record(self, kind, value):
        with self.lock:
            self.events.append({'at': time.time(), 'kind': kind, 'value': value})
            tmp = self.root / 'events.tmp'
            tmp.write_text(json.dumps(self.events, ensure_ascii=False, indent=2) + '\n')
            tmp.chmod(0o600)
            tmp.replace(self.root / 'events.json')


class Device:
    def __init__(self, name, settings, evidence):
        self.name, self.settings, self.evidence = name, settings, evidence
        self.pending, self.lock, self.write_lock = {}, threading.Lock(), threading.Lock()
        self.log = (evidence.root / f'{name}-helper-private.stderr').open('xb')
        os.chmod(self.log.name, 0o600)
        self.child = subprocess.Popen(settings['command'], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                      stderr=self.log, text=True, bufsize=1)
        self.reader = threading.Thread(target=self.read, name=f'helper-{name}', daemon=True)
        self.reader.start()
        self.server = None
        self.daemon_id = None
        self.mapping = None

    def read(self):
        try:
            while True:
                line = self.child.stdout.readline(2 << 20)
                if not line:
                    raise Failure('helper channel closed')
                require(len(line) < 2 << 20, 'helper response exceeds bounded protocol')
                message = json.loads(line)
                with self.lock:
                    channel = self.pending.get(message.get('id'))
                if channel is not None:
                    channel.put(message)
        except Exception as error:
            with self.lock:
                channels = list(self.pending.values())
            for channel in channels:
                channel.put({'ok': False, 'error_code': 'helper_channel_closed', 'private_error_type': type(error).__name__})

    def call(self, op, args=None, *, timeout=100, expect=None):
        identifier, channel = request_id(), queue.Queue(maxsize=2)
        context, label = operation_context(op, args or {})
        with self.lock:
            self.pending[identifier] = channel
        try:
            message = {'id': identifier, 'op': op, 'args': args or {}}
            with self.write_lock:
                self.child.stdin.write(json.dumps(message) + '\n')
                self.child.stdin.flush()
            response = channel.get(timeout=timeout)
            code = response.get('error_code', '')
            self.evidence.record('operation', {'device': self.name, 'op': op, **context, 'ok': response.get('ok'), 'code': code})
            if expect is not None:
                require(not response.get('ok') and code in expect, f'{self.name} {label}: expected {expect}, got {code}')
                return response
            if not response.get('ok'):
                raise OperationFailure(self.name, label, code)
            return response.get('result')
        except queue.Empty:
            raise Failure(f'{self.name} {label} timed out; never acceptance') from None
        finally:
            with self.lock:
                self.pending.pop(identifier, None)

    def rpc(self, method, message=None, *, remote=False, frontend='default', expect=None):
        return self.call('rpc', {'service': 'remote' if remote else 'agent', 'frontend': frontend,
                                'server_id': self.server, 'method': method, 'message': message or {}}, expect=expect)

    def direct_file_stat(self, project_id, path):
        return self.call('rpc', {'service': 'file-direct', 'server_id': self.server, 'method': 'StatFile',
                                'expected_daemon_id': self.daemon_id,
                                'message': {'project_id': project_id, 'path': path}})

    def cli(self, *args):
        result = self.call('cli', {'args': list(args)})
        require(result['exit_code'] == 0, f'{self.name}: CLI failed')
        return result.get('json')

    def daemon_cli(self, *args):
        result = self.call('daemon.cli', {'args': list(args)})
        require(result['exit_code'] == 0, f'{self.name}: daemon CLI failed')
        return result.get('json')

    def scope(self):
        return {'server_id': self.server, 'project_id': self.mapping['project_id'], 'mapping_id': self.mapping['id']}

    def write(self, name, content):
        return self.call('file.write', {'path': str(PurePosixPath(self.settings['files']) / name), 'content': content})

    def stat(self, path):
        return self.call('file.stat', {'path': path})

    def close(self):
        try:
            if self.child.poll() is None:
                result = self.call('cleanup', timeout=90)
                self.evidence.record('helper-cleanup', {'device': self.name, 'result': result})
                require(result.get('passed') is True, 'helper cleanup not verified')
        finally:
            if self.child.stdin:
                self.child.stdin.close()
            try:
                code = self.child.wait(timeout=30)
            except subprocess.TimeoutExpired:
                self.child.terminate()
                try:
                    self.child.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    self.child.kill()
                    self.child.wait(timeout=5)
                raise Failure('helper termination required fallback; retain remote runtime')
            self.reader.join(timeout=3)
            self.log.close()
            require(code == 0 and not self.reader.is_alive(), 'helper abnormal exit or reader retained')


class Matrix:
    def __init__(self, env, evidence):
        self.env, self.evidence = env, evidence
        self.devices = []
        self.project = self.session = None
        self.proxy = None
        self.pool = concurrent.futures.ThreadPoolExecutor(max_workers=4)
        self.cleanup_errors = []

    def record(self, kind, value):
        self.evidence.record(kind, value)

    def wait(self, description, probe, timeout=35):
        deadline = time.monotonic() + timeout
        while True:
            result = probe()
            if result:
                return result
            require(time.monotonic() < deadline, f'timed out: {description}')
            time.sleep(0.15)

    def parallel(self, *functions):
        tasks = [self.pool.submit(f) for f in functions]
        return [f.result(timeout=110) for f in tasks]

    def shared_stat(self, name):
        return self.b.stat(str(PurePosixPath(self.env['daemon']['project_root']) / name))

    def settled(self, name, content, devices=None, timeout=40):
        want = digest(content)
        def probe():
            if self.shared_stat(name).get('sha256') != want:
                return False
            for d in devices or self.devices:
                d.rpc('GetSyncState', d.scope())
                local = d.stat(str(PurePosixPath(d.settings['files']) / name))
                conflicts = d.rpc('ListConflicts', d.scope()).get('conflicts', [])
                if local.get('sha256') != want or any(c['path'] == name for c in conflicts):
                    return False
            return True
        self.wait(f'settled {name}', probe, timeout)

    def conflict(self, device, name, previous=''):
        def probe():
            values = device.rpc('ListConflicts', device.scope()).get('conflicts', [])
            return next((c for c in values if c['path'] == name and c.get('generation') and c['generation'] != previous), None)
        value = self.wait(f'{device.name} conflict {name}', probe)
        self.record('conflict', {'device': device.name, 'value': value})
        return value

    def resolve(self, device, conflict, choice, *, expect=None):
        return device.rpc('ResolveConflict', {'mapping_id': device.mapping['id'], 'conflict_id': conflict['id'],
                          'expected_generation': conflict['generation'], 'choice': choice, 'request_id': request_id()}, expect=expect)

    def pause(self, device, enabled):
        device.cli('sync', 'resume' if enabled else 'pause', device.mapping['id'])

    def terminal_state(self, device, handle, marker=None):
        return device.call('terminal.state', {'handle': handle, 'match': marker or ''})

    def terminal_marker(self, marker):
        self.b.call('terminal.send', {'handle': 'b', 'text': marker + '\n'})
        # The isolated shell fixture emits this prefix; matching input alone
        # would accept terminal local echo without proving command execution.
        reply = 'MULTIHOST_REPLY:' + marker
        result = self.wait('B terminal marker', lambda: (s if (s := self.terminal_state(self.b, 'b', reply)).get('matched') else None))
        self.record('terminal-marker', {'marker': marker, 'result': result})

    @staticmethod
    def display_verified(display, expected):
        checks = display.get('checks', {})
        return display.get('exit_code') == 0 and bool(expected) and all(checks.get(value) is True for value in expected)

    def open_term(self, device, name, frontend='default', read_only=False):
        # The daemon limits redraws to one per 100 ms. Retry only its explicit
        # admission throttle; attachment caps or persistent throttling still fail.
        for attempt in range(5):
            try:
                return device.call('terminal.open', {'handle': name, 'frontend': frontend, 'server_id': device.server,
                                  'session_id': self.session['id'], 'read_only': read_only, 'device_name': 'multihost-' + device.name})
            except OperationFailure as error:
                if error.code != 'resource_exhausted' or attempt == 4:
                    raise
                time.sleep(0.15)

    def setup(self):
        self.a = Device('a', self.env['a'], self.evidence)
        self.devices.append(self.a)
        self.b = Device('b', self.env['b'], self.evidence)
        self.devices.append(self.b)
        identities = [d.call('ping') for d in self.devices]
        require(identities[0]['os'] == 'darwin' and identities[1]['os'] == 'linux', 'expected native Mac and Linux clients')
        require(all(int(i['uid']) != 0 for i in identities), 'helpers must run as ordinary users')
        self.record('device-identities', identities)
        self.b.call('process.start', {'role': 'daemon'})
        self.proxy = self.a.call('proxy.start', {'target': self.env['a']['tls_target']})
        for d in self.devices:
            d.call('process.start', {'role': 'agent'})
            d.call('frontend.open', {'name': 'default'})
            address = self.proxy['address'] if d is self.a else d.settings['tls_target']
            d.cli('connect', 'multihost', '--tls-address', address, '--tls-server-name', 'localhost',
                  '--tls-ca', d.settings['ca_file'], '--token-file', d.settings['token_file'])
            servers = d.rpc('ListServers')['servers']
            require(len(servers) == 1 and servers[0]['credential_present'], 'isolated agent must contain one test server')
            d.server = servers[0]['config']['id']
            require(servers[0]['remote_protocol']['build_version'] == self.env['expected_version'], 'wrong daemon binary')
            d.daemon_id = servers[0]['remote_protocol'].get('daemon_id')
            require(d.daemon_id, 'authenticated daemon identity required for direct file primitive verification')
        self.project = self.a.rpc('CreateProject', {'name': 'multihost-' + self.env['run_id'],
                                    'path': self.env['daemon']['project_root'], 'request_id': request_id()}, remote=True)
        self.session = self.a.rpc('CreateSession', {'name': 'multihost-terminal', 'project_id': self.project['id'],
                                                  'request_id': request_id()}, remote=True)
        self.record('owned-resources', {'project': self.project, 'session': self.session})
        self.seeded = ['baseline.txt', 'conditional.txt', 'offline.txt', 'deleted.txt', 'equal.txt',
                       'remote-only.txt', 'keep-both.txt', 'upload-crash.bin', 'resolve-crash.bin',
                       'concurrent-a.txt', 'concurrent-b.txt', 'copy-collision.txt']
        for name in self.seeded:
            for d in self.devices:
                d.write(name, 'baseline\n')
            self.b.call('file.write', {'path': self.env['daemon']['project_root'] + '/' + name, 'content': 'baseline\n'})
        self.a.cli('plugins', 'enable', 'sync', '--side', 'remote', '--server', 'multihost')
        self.a.cli('plugins', 'enable', 'port', '--side', 'remote', '--server', 'multihost')
        for d in self.devices:
            for plugin in ('sync', 'port'):
                d.cli('plugins', 'enable', plugin, '--side', 'local')
            d.cli('sync', 'map', self.project['id'], '--local', d.settings['files'], '--server', 'multihost')
            mappings = d.rpc('ListMappings')['mappings']
            require(len(mappings) == 1, 'exactly one private mapping expected')
            d.mapping = mappings[0]
            self.pause(d, True)
        require(self.a.mapping['id'] != self.b.mapping['id'], 'mapping identity reused across devices')
        self.record('mapping-isolation', [d.mapping for d in self.devices])
        self.settled('baseline.txt', 'baseline\n')

    def terminals(self):
        self.a.call('frontend.open', {'name': 'initial'})
        initial = self.open_term(self.a, 'initial', 'initial')
        ta, tb = self.open_term(self.a, 'a'), self.open_term(self.b, 'b')
        self.old_a_attachment = ta['attachment_id']
        require(initial['mode'] == 'controller' and ta['mode'] == tb['mode'] == 'observer', 'incorrect initial terminal ownership')
        self.b.call('frontend.open', {'name': 'reader'})
        reader = self.open_term(self.b, 'reader', 'reader', True)
        before = self.a.rpc('GetSession', {'id': self.session['id']}, remote=True)
        def take(d, attachment, frontend='default', expect=None):
            return d.rpc('TakeControl', {'session_id': self.session['id'], 'attachment_id': attachment,
                         'expected_control_revision': before['control_revision'], 'request_id': request_id()},
                         remote=True, frontend=frontend, expect=expect)
        take(self.b, reader['attachment_id'], 'reader', expect=('permission_denied',))
        def contender(d, attachment):
            try:
                take(d, attachment)
                return 'success'
            except Failure as error:
                if 'aborted' in str(error):
                    return 'aborted'
                raise
        results = self.parallel(lambda: contender(self.a, ta['attachment_id']), lambda: contender(self.b, tb['attachment_id']))
        require(sorted(results) == ['aborted', 'success'], 'same-revision control race did not have one winner')
        current = self.b.rpc('GetSession', {'id': self.session['id']}, remote=True)
        self.b.rpc('TakeControl', {'session_id': self.session['id'], 'attachment_id': tb['attachment_id'],
                   'expected_control_revision': current['control_revision'], 'request_id': request_id()}, remote=True)
        self.terminal_marker('MULTIHOST_CONTROL_OK')
        for mode in ('read_only', 'take_control'):
            # The actual CLI is launched once: allow the prior attachment's
            # documented redraw interval to elapse rather than retrying its UI.
            time.sleep(0.15)
            result = self.a.call('terminal.ui', {'server_id': self.a.server, 'project_id': self.project['id'],
                                 'session_id': self.session['id'], mode: True})
            require(result.get('matched') is True and result.get('exit_code') == 0, 'CLI terminal mode or detach failed')
            self.record('terminal-cli-mode', {'mode': mode, 'result': result})
        current = self.b.rpc('GetSession', {'id': self.session['id']}, remote=True)
        self.b.rpc('TakeControl', {'session_id': self.session['id'], 'attachment_id': tb['attachment_id'],
                   'expected_control_revision': current['control_revision'], 'request_id': request_id()}, remote=True)
        self.terminal_marker('MULTIHOST_B_CONTROL_AFTER_CLI_UI')
        self.original_pane = self.b.rpc('GetSession', {'id': self.session['id']}, remote=True)
        self.record('takeover-race', {'results': results, 'before': before, 'after': self.original_pane})
        for d in self.devices:
            expected = ['multihost-terminal', 'attachments']
            display = d.call('tui.capture', {'server_id': d.server, 'project_id': self.project['id'],
                             'tabs': 0, 'contains': expected})
            require(self.display_verified(display, expected), 'Sessions TUI omitted session or attachment count')
            self.record('sessions-display', {'device': d.name, 'tui': display})

    def concurrent_paths(self):
        self.parallel(lambda: self.a.write('concurrent-a.txt', 'independent A edit\n'),
                      lambda: self.b.write('concurrent-b.txt', 'independent B edit\n'))
        self.settled('concurrent-a.txt', 'independent A edit\n')
        self.settled('concurrent-b.txt', 'independent B edit\n')
        self.record('concurrent-independent-paths', {'both_devices_converged': True})

    def config_cas(self):
        self.a.call('frontend.open', {'name': 'window2'})
        before = self.a.rpc('GetPreferences')
        first = dict(before, no_color=not before['no_color'])
        self.a.rpc('SetPreferences', {'values': first, 'request_id': request_id()})
        self.a.rpc('SetPreferences', {'values': dict(before, detach_key='ctrl-]'), 'request_id': request_id()},
                   frontend='window2', expect=('aborted',))
        current = self.a.rpc('GetPreferences')
        require(current['revision'] != before['revision'] and current['no_color'] == first['no_color'], 'config CAS result lost')
        overlap = dict(self.a.mapping, id='', revision='', local_root=self.a.settings['files'] + '/overlap')
        self.a.rpc('PutMapping', {'mapping': overlap, 'request_id': request_id()}, expect=('already_exists', 'invalid_argument', 'failed_precondition'))
        self.record('local-config-cas', {'before': before, 'after': current, 'overlap_rejected': True})

    def conditional_writes(self):
        for name, initial, values in [('conditional.txt', 'old\n', ['one\n', 'two\n']),
                                       ('create-different.txt', None, ['first\n', 'second\n']),
                                       ('create-identical.txt', None, ['same\n', 'same\n'])]:
            for d in self.devices:
                self.pause(d, False)
            base = ''
            if initial is not None:
                self.b.call('file.write', {'path': self.env['daemon']['project_root'] + '/' + name, 'content': initial})
                base = self.b.direct_file_stat(self.project['id'], name)['revision']
            else:
                for device, value in zip(self.devices, values):
                    device.write(name, value)
            def upload(d, value):
                try:
                    result = d.call('upload', {'service': 'file-direct', 'server_id': d.server,
                                    'expected_daemon_id': d.daemon_id, 'project_id': self.project['id'],
                                    'path': name, 'base_revision': base, 'content': value})
                    return {'status': 'success', 'result': result, 'hash': digest(value)}
                except Failure as error:
                    require('aborted' in str(error) or 'already_exists' in str(error), 'unexpected conditional upload failure')
                    return {'status': 'rejected'}
            results = self.parallel(lambda: upload(self.a, values[0]), lambda: upload(self.b, values[1]))
            require(sum(r['status'] == 'success' for r in results) == 1, 'conditional writes did not serialize to one commit')
            winner = next(r for r in results if r['status'] == 'success')
            require(self.shared_stat(name)['sha256'] == winner['hash'], 'losing write overwrote conditional winner')
            for d in self.devices:
                self.pause(d, True)
            if initial is None:
                # A matching independently created local file must adopt the
                # common content. A differing loser requires explicit choice.
                for device, value in zip(self.devices, values):
                    if digest(value) != winner['hash']:
                        conflict = self.conflict(device, name)
                        self.resolve(device, conflict, 'KEEP_REMOTE')
            self.settled(name, values[0] if digest(values[0]) == winner['hash'] else values[1])
            self.record('conditional-write', {'path': name, 'results': results, 'submission_route': 'authenticated file-direct'})

    def offline_conflicts(self):
        for name in ('offline.txt', 'deleted.txt', 'equal.txt', 'remote-only.txt'):
            self.a.write(name, 'base\n')
            self.settled(name, 'base\n')
        self.a.call('process.stop', {'role': 'agent'})
        self.a.write('offline.txt', 'local edit\n')
        self.a.write('deleted.txt', 'local survivor\n')
        self.a.write('equal.txt', 'same new\n')
        self.b.write('offline.txt', 'remote edit\n')
        self.b.write('equal.txt', 'same new\n')
        self.b.write('remote-only.txt', 'remote only\n')
        self.b.call('file.remove', {'path': self.b.settings['files'] + '/deleted.txt'})
        for name, value in [('offline.txt', 'remote edit\n'), ('equal.txt', 'same new\n'), ('remote-only.txt', 'remote only\n')]:
            self.settled(name, value, [self.b])
        self.wait('remote delete', lambda: not self.shared_stat('deleted.txt')['exists'])
        self.terminal_marker('MULTIHOST_B_SURVIVES_A_OFFLINE')
        self.a.call('process.start', {'role': 'agent'})
        self.a.call('frontend.open', {'name': 'default'})
        reopened = self.open_term(self.a, 'a-reconnected')
        require(reopened['attachment_id'] != self.old_a_attachment and reopened['mode'] == 'observer',
                'reconnected A reused its attachment or stole B control')
        current = self.b.rpc('GetSession', {'id': self.session['id']}, remote=True)
        self.a.rpc('TakeControl', {'session_id': self.session['id'], 'attachment_id': self.old_a_attachment,
                   'expected_control_revision': current['control_revision'], 'request_id': request_id()},
                   remote=True, expect=('permission_denied',))
        self.record('attachment-renewal', {'old': self.old_a_attachment, 'new': reopened,
                                          'old_attachment_rejected': True, 'rejection_boundary': 'agent frontend ownership', 'cli_input_replay_tested': False})
        conflict = self.conflict(self.a, 'offline.txt')
        deleted = self.conflict(self.a, 'deleted.txt')
        require(not deleted['remote']['exists'] and deleted['local']['exists'], 'modify/delete did not retain deletion conflict')
        require(conflict['base']['content_hash'] == digest('base\n') and conflict['local']['content_hash'] == digest('local edit\n')
                and conflict['remote']['content_hash'] == digest('remote edit\n'), 'three conflict versions not preserved')
        # The wire API has LOCAL/REMOTE preview only; the common base is
        # represented by Conflict.Base metadata, not a fabricated BASE stream.
        for side, value in [('local', 'local edit\n'), ('remote', 'remote edit\n')]:
            preview = self.a.call('conflict.preview', {'frontend': 'default', 'mapping_id': self.a.mapping['id'],
                                  'conflict_id': conflict['id'], 'generation': conflict['generation'], 'side': side})
            require(preview['sha256'] == digest(value), 'versioned conflict preview mismatch')
        self.settled('equal.txt', 'same new\n')
        self.settled('remote-only.txt', 'remote only\n')
        self.b.write('offline.txt', 'third remote edit\n')
        self.settled('offline.txt', 'third remote edit\n', [self.b])
        self.resolve(self.a, conflict, 'KEEP_LOCAL', expect=('aborted',))
        newer = self.conflict(self.a, 'offline.txt', conflict['generation'])
        self.a.write('offline.txt', 'third local edit\n')
        self.resolve(self.a, newer, 'KEEP_REMOTE', expect=('aborted',))
        final = self.conflict(self.a, 'offline.txt', newer['generation'])
        self.resolve(self.a, final, 'KEEP_REMOTE')
        self.settled('offline.txt', 'third remote edit\n')
        self.resolve(self.a, deleted, 'KEEP_LOCAL')
        self.settled('deleted.txt', 'local survivor\n')
        self.record('offline-and-stale-resolution', {'same_content_adopted': True, 'remote_only_downloaded': True,
                    'modify_delete_preserved': True, 'stale_remote_and_local_generations_rejected': True})

    def keep_both_and_ui(self):
        self.pause(self.a, False)
        self.a.write('keep-both.txt', 'A keeps this version\n')
        self.b.write('keep-both.txt', 'B keeps that version\n')
        self.settled('keep-both.txt', 'B keeps that version\n', [self.b])
        self.pause(self.a, True)
        conflict = self.conflict(self.a, 'keep-both.txt')
        # No frontend must be necessary for the conflict's persistence.
        for name in ('default', 'initial', 'window2'):
            self.a.call('frontend.close', {'name': name})
        expected = [self.a.settings['files'], 'keep-both.txt', conflict['generation']]
        display = self.a.call('tui.capture', {'server_id': self.a.server, 'project_id': self.project['id'],
                             'tabs': 1, 'contains': expected})
        require(self.display_verified(display, expected), 'actual TUI omitted local path or persistent conflict generation')
        self.a.call('frontend.open', {'name': 'default'})
        persisted = self.conflict(self.a, 'keep-both.txt')
        require(persisted['generation'] == conflict['generation'], 'closing frontends changed conflict generation')
        before = self.a.call('file.list', {'path': self.a.settings['files'], 'recursive': True})['files']
        self.resolve(self.a, persisted, 'KEEP_BOTH')
        def copies():
            result = self.a.call('file.list', {'path': self.a.settings['files'], 'recursive': True})['files']
            hashes = {p['sha256'] for p in result}
            return result if {digest('A keeps this version\n'), digest('B keeps that version\n')} <= hashes else None
        after = self.wait('KEEP_BOTH preserves both payloads', copies)
        old = {p['path']: p['sha256'] for p in before if p['path'] != 'keep-both.txt'}
        new = {p['path']: p['sha256'] for p in after}
        require(all(new.get(path) == value for path, value in old.items()), 'KEEP_BOTH overwrote an existing unrelated file')
        self.settled('keep-both.txt', 'A keeps this version\n')
        copies = [p['path'] for p in after if p['path'] not in {p['path'] for p in before}
                  and p['sha256'] == digest('B keeps that version\n')]
        require(len(copies) == 1, 'KEEP_BOTH must create one unique remote-version copy')
        # B has no common baseline for the new copy and must explicitly adopt
        # it; preserving both versions on A alone is not convergence evidence.
        self.resolve(self.b, self.conflict(self.b, copies[0]), 'KEEP_REMOTE')
        self.settled(copies[0], 'B keeps that version\n')
        self.record('keep-both-and-ui', {'before': before, 'after': after, 'tui': display})

    def forwards(self):
        self.echo = self.b.call('echo.start')
        target = {'side': 'REMOTE', 'server_id': self.a.server}
        port = next(p for p in self.a.rpc('ListPlugins', target)['plugins'] if p['plugin_id'] == 'port')
        self.a.rpc('ManagePlugin', {'target': target, 'request': {'plugin_id': 'port', 'action': 'SET_CONFIG',
                   'config': {'allow_ports': [self.echo['port']], 'idle_timeout_seconds': 120},
                   'expected_revision': port['revision'], 'request_id': request_id()}})
        self.wait('remote port worker ready with exact echo allowlist', lambda: next((p for p in
                  self.a.rpc('ListPlugins', target)['plugins'] if p['plugin_id'] == 'port' and p['state'] == 'ready'), None))
        self.forward = {}
        for d in self.devices:
            result = d.rpc('CreateForward', {'request_id': request_id(), 'spec': {'server_id': d.server,
                          'remote_address': '127.0.0.1', 'remote_port': self.echo['port'],
                          'local_address': '127.0.0.1', 'local_port': 0, 'enabled': True}})
            identifier = result['spec']['id']
            def listening():
                values = d.rpc('ListForwards')['forwards']
                return next((v for v in values if v['spec']['id'] == identifier and v['state'] == 'listening'
                             and int(v['bound_port']) > 0), None)
            state = self.wait('independent loopback listener', listening)
            require(state['spec']['local_address'] == '127.0.0.1', 'non-loopback listener')
            self.forward[d.name] = state
            d.call('tcp.open', {'handle': 'forward-' + d.name, 'address': '127.0.0.1:' + str(state['bound_port']),
                               'marker': 'FORWARD_' + d.name + '\n'})
            self.wait('forward echo', lambda: d.call('tcp.state', {'handle': 'forward-' + d.name, 'match': 'FORWARD_' + d.name}).get('matched'))
        state = self.forward['a']
        self.a.rpc('StopForward', {'forward_id': state['spec']['id'], 'expected_revision': state['spec']['revision'], 'request_id': request_id()})
        self.wait('A stopped TCP', lambda: self.a.call('tcp.state', {'handle': 'forward-a'}).get('closed'))
        self.b.call('tcp.send', {'handle': 'forward-b', 'text': 'B_AFTER_A_STOP\n'})
        self.wait('B independent forward', lambda: self.b.call('tcp.state', {'handle': 'forward-b', 'match': 'B_AFTER_A_STOP'}).get('matched'))
        # Recreate A's forward for the later per-device credential revocation.
        self.forward['a'] = self.a.rpc('CreateForward', {'request_id': request_id(), 'spec': {
            'server_id': self.a.server, 'remote_address': '127.0.0.1', 'remote_port': self.echo['port'],
            'local_address': '127.0.0.1', 'local_port': 0, 'enabled': True}})
        for d in self.devices:
            states = d.rpc('ListForwards')['forwards']
            require(all(v['spec']['server_id'] == d.server for v in states), 'forward crosses device server identity')
            display = d.call('tui.capture', {'server_id': d.server, 'project_id': self.project['id'],
                             'tabs': 2, 'contains': ['127.0.0.1', 'listening']})
            require(self.display_verified(display, ['127.0.0.1', 'listening']), 'TUI does not show own listener')
            self.record('ports-display', {'device': d.name, 'forwards': states, 'tui': display})

    def partial(self, pattern, size):
        files = self.b.call('file.list', {'path': self.env['daemon']['project_root'], 'recursive': True})['files']
        for item in files:
            count = int(item['size'])
            if '.shiki-sync-transfer-' in item['path'] and 0 < count < size and item['sha256'] == digest(payload(pattern, count)):
                return item
        return None

    def hard_crash(self, phase, filename, pattern, *, resolve=None):
        self.a.call('proxy.mode', {'handle': self.proxy['handle'], 'slow': True, 'paused': False})
        size = 8 << 20
        before = self.shared_stat(filename)
        identities = self.a.call('process.inventory')
        own = [p for p in identities['identities'] if Path(p['executable']).name in ('shiki-agent', 'shiki-plugin-sync', 'shiki-plugin-port')]
        require(len(own) == 3, 'hard crash requires one exact agent and both workers')
        old_status = self.a.rpc('GetStatus')
        b_status = self.b.rpc('GetStatus')
        snapshot_args = {'mapping_id': self.a.mapping['id'], 'path': filename}
        journal_before = self.a.call('sync.snapshot', snapshot_args)
        require(journal_before.get('operation') is None and journal_before.get('baseline') is not None,
                'hard crash requires an established baseline and no prior operation for its path')
        self.record('crash-initial-journal', {'phase': phase, 'snapshot': journal_before})
        future = None
        if resolve is None:
            self.a.call('file.write', {'path': self.a.settings['files'] + '/' + filename, 'pattern': pattern, 'size': size})
        else:
            future = self.pool.submit(lambda: self.resolve(self.a, resolve, 'KEEP_LOCAL'))
        staged = self.wait('real partial upload before SIGKILL', lambda: self.partial(pattern, size), timeout=50)
        self.a.call('proxy.mode', {'handle': self.proxy['handle'], 'slow': True, 'paused': True})
        # Check after the gate is closed: completion during the observation
        # window is a failed precondition, not a simulated crash acceptance.
        require(self.shared_stat(filename)['sha256'] == before['sha256'], 'transfer committed before hard-crash boundary')
        confirmed = self.partial(pattern, size)
        require(confirmed is not None, 'partial stage disappeared before SIGKILL')
        prepared = self.a.call('sync.snapshot', snapshot_args)
        operation = prepared.get('operation') or {}
        stage = operation.get('stage_file') or {}
        require(operation.get('phase') == 'prepared' and operation.get('target_path') == filename
                and operation.get('source_side') == 'local' and operation.get('target_side') == 'remote',
                'partial upload lacks a durable prepared operation for the exact path')
        require(prepared.get('baseline') == journal_before['baseline'], 'prepared upload advanced its baseline before commit')
        require(operation.get('source', {}).get('content_hash') == digest(payload(pattern, size))
                and operation.get('target', {}).get('content_hash') == before['sha256']
                and stage.get('exists') is True and int(stage.get('size', -1)) == size
                and stage.get('sha256') == digest(payload(pattern, size)),
                'prepared operation lacks the complete verified source snapshot or expected remote target')
        # A sender can already mark commit_state=possible while TLS bytes are
        # buffered. The observed remote partial file defines this crash point;
        # it does not prove the separate commit-success/ACK-loss scenario.
        self.record('crash-prepared-journal', {'phase': phase, 'snapshot': prepared})
        killed = self.a.call('process.kill', {'role': 'agent'})
        self.a.call('proxy.mode', {'handle': self.proxy['handle'], 'slow': False, 'paused': False})
        retired = self.wait('old agent and workers retired', lambda: (r if (r := self.a.call('retired.check', {'identities': own})).get('all_absent') else None), timeout=25)
        # SIGKILL cannot unlink the host's UDS paths. Exact process retirement
        # proves the old owners are gone; startup owns stale-path recovery.
        for socket in identities.get('sockets', []):
            require(self.a.call('socket.probe', {'path': socket})['connectable'] is False,
                    'old owned listener still accepts connections after hard crash')
        require(self.b.rpc('GetStatus')['instance_id'] == b_status['instance_id'], 'A hard crash restarted B')
        self.terminal_marker('B_SURVIVES_' + phase.upper())
        if hasattr(self, 'echo'):
            self.b.call('tcp.send', {'handle': 'forward-b', 'text': 'B_TCP_' + phase.upper() + '\n'})
            self.wait('B TCP survives A SIGKILL', lambda: self.b.call('tcp.state', {'handle': 'forward-b', 'match': 'B_TCP_' + phase.upper()}).get('matched'))
        if future is not None:
            try:
                future.result(timeout=20)
            except Failure:
                pass  # Expected interruption, followed by disk/RPC reconciliation.
            else:
                raise Failure('resolution reported success despite precommit SIGKILL')
        self.a.call('process.start', {'role': 'agent'})
        self.a.call('frontend.open', {'name': 'default'})
        require(self.a.rpc('GetStatus')['instance_id'] != old_status['instance_id'], 'restart reused crashed instance')
        def recovered():
            # ListConflicts can expose the durable pre-crash entry before the
            # new worker has scanned it. Wait for live reconciliation, not just
            # frontend readiness; never replay a write on Canceled.
            status = self.a.rpc('GetSyncState', self.a.scope())
            if (status.get('mapping_revision') != prepared['mapping_revision']
                    or status.get('state') not in ('active', 'conflicted')):
                return None
            snapshot = self.a.call('sync.snapshot', snapshot_args)
            if operation.get('commit_state') == 'incomplete' and snapshot.get('operation') is not None:
                return None  # This unchanged source/target must recover itself.
            conflict = next((c for c in self.a.rpc('ListConflicts', self.a.scope()).get('conflicts', []) if c['path'] == filename), None)
            if conflict:
                require(conflict['local']['content_hash'] == digest(payload(pattern, size)), 'crash lost local payload')
                require(conflict['remote']['content_hash'] == before['sha256'], 'crash silently overwrote remote conflict')
                return ('conflict', conflict)
            if self.shared_stat(filename).get('sha256') == digest(payload(pattern, size)):
                return ('committed', None)
            return None
        state, conflict = self.wait('crash recovery reconciliation', recovered, timeout=60)
        if conflict:
            self.resolve(self.a, conflict, 'KEEP_LOCAL')
        self.settled(filename, payload(pattern, size), timeout=60)
        remote = self.b.direct_file_stat(self.project['id'], filename)
        def committed_baseline():
            snapshot = self.a.call('sync.snapshot', snapshot_args)
            baseline = snapshot.get('baseline') or {}
            if (snapshot.get('operation') is None and baseline.get('exists') is True
                    and baseline.get('content_hash') == digest(payload(pattern, size))
                    and baseline.get('revision') == remote['revision']):
                return snapshot
            return None
        journal_after = self.wait('recovered durable baseline without pending operation', committed_baseline)
        current_pane = self.b.rpc('GetSession', {'id': self.session['id']}, remote=True)
        require(current_pane['claude_pid'] == self.original_pane['claude_pid'] and current_pane['tmux_pane'] == self.original_pane['tmux_pane'],
                'client crash replaced daemon terminal pane')
        self.record('hard-crash', {'phase': phase, 'signal': 'SIGKILL', 'before': before, 'partial': confirmed,
                    'declared_bytes': size, 'killed': killed, 'retired': retired, 'recovery': state,
                    'journal_before': journal_before, 'journal_prepared': prepared, 'journal_after': journal_after,
                    'new_status': self.a.rpc('GetStatus'), 'unknown_commit_ack_loss_tested': False})

    def crashes(self):
        self.hard_crash('upload', 'upload-crash.bin', 'ordinary-upload-')
        self.pause(self.a, False)
        self.a.call('file.write', {'path': self.a.settings['files'] + '/resolve-crash.bin', 'pattern': 'resolve-upload-', 'size': 8 << 20})
        self.b.write('resolve-crash.bin', 'remote version to preserve\n')
        self.settled('resolve-crash.bin', 'remote version to preserve\n', [self.b])
        self.pause(self.a, True)
        conflict = self.conflict(self.a, 'resolve-crash.bin')
        self.hard_crash('resolution', 'resolve-crash.bin', 'resolve-upload-', resolve=conflict)

    def copy_collision(self):
        # Keep the collision window inside the documented 5s frontend unary
        # budget. The separate SIGKILL scenarios still transfer 8 MiB.
        name, pattern, size = 'copy-collision.txt', 'remote-copy-payload-', 1 << 20
        self.pause(self.a, False)
        self.a.write(name, 'A collision local version\n')
        self.b.call('file.write', {'path': self.b.settings['files'] + '/' + name, 'pattern': pattern, 'size': size})
        self.settled(name, payload(pattern, size), [self.b], timeout=60)
        self.pause(self.a, True)
        conflict = self.conflict(self.a, name)
        self.a.call('proxy.mode', {'handle': self.proxy['handle'], 'slow': True, 'paused': False})
        pending = self.pool.submit(lambda: self.resolve(self.a, conflict, 'KEEP_BOTH'))
        def local_copy():
            if pending.done():
                pending.result()  # Surface an early RPC failure instead of polling.
                raise Failure('KEEP_BOTH completed before collision injection')
            files = self.a.call('file.list', {'path': self.a.settings['files'], 'recursive': True})['files']
            return next((f for f in files if f['path'].startswith(name + '.conflict-')
                         and f['sha256'] == digest(payload(pattern, size))), None)
        try:
            copy = self.wait('local copy reveals exact pending remote copy target', local_copy, timeout=50)
            require(not self.shared_stat(copy['path'])['exists'], 'remote copy committed before collision injection')
            sentinel = 'existing same-name remote copy must survive\n'
            self.b.call('file.write', {'path': self.env['daemon']['project_root'] + '/' + copy['path'], 'content': sentinel})
            self.a.call('proxy.mode', {'handle': self.proxy['handle'], 'slow': False, 'paused': False})
            try:
                pending.result(timeout=90)
            except Failure as error:
                require('aborted' in str(error) or 'already_exists' in str(error), 'unexpected KEEP_BOTH collision error')
            else:
                raise Failure('KEEP_BOTH accepted a concurrent same-name destination')
            require(self.shared_stat(copy['path'])['sha256'] == digest(sentinel), 'KEEP_BOTH overwrote collision sentinel')
            require(self.a.stat(self.a.settings['files'] + '/' + name)['sha256'] == digest('A collision local version\n'),
                    'copy collision lost original local version')
            require(self.shared_stat(name)['sha256'] == digest(payload(pattern, size)), 'copy collision overwrote original remote version')
            self.record('keep-both-copy-collision', {'copy_path': copy['path'], 'destination_sentinel_preserved': True,
                        'original_local_and_remote_preserved': True, 'pending_conflict_archived_on_cleanup': True})
        finally:
            self.a.call('proxy.mode', {'handle': self.proxy['handle'], 'slow': False, 'paused': False})

    def revoke_a(self):
        ta = self.open_term(self.a, 'a-revoked')
        state = self.wait('A forward listening before revoke', lambda: next((f for f in self.a.rpc('ListForwards')['forwards']
                         if f['spec']['id'] == self.forward['a']['spec']['id'] and f['state'] == 'listening'), None))
        self.a.call('tcp.open', {'handle': 'revoke-a', 'address': '127.0.0.1:' + str(state['bound_port']), 'marker': 'A_BEFORE_REVOKE\n'})
        self.wait('A forward authenticated before revoke', lambda: self.a.call('tcp.state', {'handle': 'revoke-a', 'match': 'A_BEFORE_REVOKE'}).get('matched'))
        b_status = self.b.rpc('GetStatus')
        self.b.daemon_cli('token', 'revoke', self.env['daemon']['token_id_a'])
        self.wait('A terminal ends after token revoke', lambda: self.terminal_state(self.a, 'a-revoked').get('closed'))
        self.wait('A TCP ends after token revoke', lambda: self.a.call('tcp.state', {'handle': 'revoke-a'}).get('closed'))
        self.a.rpc('GetProject', {'id': self.project['id']}, remote=True, expect=('unauthenticated', 'permission_denied'))
        self.terminal_marker('B_SURVIVES_TOKEN_A_REVOKED')
        self.b.call('tcp.send', {'handle': 'forward-b', 'text': 'B_TCP_AFTER_REVOKE\n'})
        self.wait('B TCP retains own credential', lambda: self.b.call('tcp.state', {'handle': 'forward-b', 'match': 'B_TCP_AFTER_REVOKE'}).get('matched'))
        self.b.write('baseline.txt', 'B sync after A revoke\n')
        self.settled('baseline.txt', 'B sync after A revoke\n', [self.b])
        require(self.b.rpc('GetStatus')['instance_id'] == b_status['instance_id'], 'revoking A restarted B')
        self.record('token-revocation-isolation', {'a_attachment': ta['attachment_id'], 'b_status': b_status,
                    'a_terminal_and_tcp_closed': True, 'b_terminal_tcp_sync_continued': True})

    def cleanup(self):
        # Every step is independent, so a revoked credential or failed assertion
        # cannot prevent closing unrelated owned streams/processes on either host.
        def attempt(label, function):
            try:
                function()
            except Exception as error:
                self.cleanup_errors.append({'step': label, 'error_type': type(error).__name__})
        def retire_mapping(device):
            self.pause(device, False)
            current = next(m for m in device.rpc('ListMappings')['mappings'] if m['id'] == device.mapping['id'])
            device.rpc('RemoveMapping', {'mapping_id': current['id'], 'expected_revision': current['revision'],
                                         'archive_pending': True, 'request_id': request_id()})
        for device in self.devices:
            if device.mapping:
                attempt('archive-owned-mapping-' + device.name, lambda device=device: retire_mapping(device))
        if hasattr(self, 'b') and self.b.server:
            if self.session:
                attempt('delete-owned-session', lambda: self.b.rpc('DeleteSession', {'id': self.session['id']}, remote=True))
            if self.project:
                attempt('delete-owned-project', lambda: self.b.rpc('DeleteProject', {'id': self.project['id']}, remote=True))
            for token_id in (self.env['daemon']['token_id_a'], self.env['daemon']['token_id_b']):
                attempt('revoke-owned-token', lambda token_id=token_id: self.b.daemon_cli('token', 'revoke', token_id))
        for device in reversed(self.devices):
            attempt('close-helper-' + device.name, device.close)
        self.pool.shutdown(wait=True, cancel_futures=True)
        self.record('controller-cleanup', {'passed': not self.cleanup_errors, 'errors': self.cleanup_errors,
                    'account_directory_and_ssh_cleanup': 'operator-owned final step; not performed by controller'})

    def run(self):
        self.setup()
        for name in ('terminals', 'config_cas', 'concurrent_paths', 'forwards', 'crashes', 'conditional_writes',
                     'offline_conflicts', 'keep_both_and_ui', 'copy_collision', 'revoke_a'):
            self.record('phase-start', name)
            getattr(self, name)()
            self.record('phase-complete', name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--environment', required=True, type=Path)
    parser.add_argument('--check', action='store_true', help='validate JSON only; no processes or mutations')
    arguments = parser.parse_args()
    env = json.loads(arguments.environment.read_text())
    validate(env)
    if arguments.check:
        print(json.dumps({'valid': True, 'execution_started': False}))
        return
    evidence = Evidence(env['evidence_dir'])
    evidence.record('scope', {'run_id': env['run_id'], 'version': env['expected_version'], 'physical_clients': ['native Mac', 'remote Debian VPS'],
                    'transport': 'application TLS over a private SSH TCP bridge for A; loopback TLS for B',
                    'file_primitives': 'conditional WriteFile and verification StatFile use authenticated direct TLS with the same daemon identity',
                    'tsnet_acceptance': False, 'real_claude_acceptance': False, 'products_rebuilt_by_controller': False})
    matrix, passed, failure = Matrix(env, evidence), False, None
    started = time.monotonic()
    try:
        matrix.run()
        passed = True
    except Exception as error:
        failure = {'type': type(error).__name__, 'message': str(error)[:300] if isinstance(error, Failure) else 'private harness failure'}
    finally:
        matrix.cleanup()
        passed = passed and not matrix.cleanup_errors
        evidence.record('result', {'passed': passed, 'failure': failure, 'elapsed_seconds': time.monotonic() - started,
                        'external_environment_cleanup_complete': False})
    print(json.dumps({'passed': passed, 'evidence': str(evidence.root)}))
    if not passed:
        raise SystemExit(1)


if __name__ == '__main__':
    main()

