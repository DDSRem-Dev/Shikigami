#!/usr/bin/env python3
"""Clean only the recorded native schema fixture; never kill a tmux server."""
import json
import os
from pathlib import Path
import plistlib
import pwd
import subprocess
import sys
import time


ACTIONS = []

def run(args, **kwargs):
    return subprocess.run(args, capture_output=True, text=True, timeout=15, **kwargs)


def job_absent(domain, target):
    if run(['/bin/launchctl', 'print', domain]).returncode != 0:
        raise RuntimeError('GUI domain unreadable; job absence unproven')
    state = run(['/bin/launchctl', 'print', target])
    if state.returncode == 0:
        return False
    if 'Could not find service' not in state.stderr:
        raise RuntimeError('cannot establish exact LaunchAgent absence')
    return True


def clean_job(root):
    home = Path(pwd.getpwuid(os.getuid()).pw_dir)
    plist = home / 'Library/LaunchAgents/dev.shikigami.agent.plist'
    program = str(root / 'runtime/install/current/shiki-agent')
    domain = f'gui/{os.getuid()}'
    target = domain + '/dev.shikigami.agent'
    owned = False
    if plist.exists() or plist.is_symlink():
        if plist.is_symlink():
            raise RuntimeError('refusing symlink service file')
        value = plistlib.loads(plist.read_bytes())
        owned = value.get('ProgramArguments', [None])[0] == program
        if not owned:
            raise RuntimeError('foreign service file; refusing cleanup')
    state = run(['/bin/launchctl', 'print', target])
    if state.returncode == 0:
        if f'program = {program}\n' not in state.stdout:
            raise RuntimeError('foreign service job; refusing cleanup')
        ACTIONS.append({'action': 'bootout', 'target': target})
        removed = run(['/bin/launchctl', 'bootout', target])
        if removed.returncode != 0:
            raise RuntimeError('owned job bootout failed: ' + removed.stderr)
    deadline = time.monotonic() + 10
    while not job_absent(domain, target):
        if time.monotonic() >= deadline:
            raise RuntimeError('owned job remained registered')
        time.sleep(0.1)
    if owned:
        if plist.is_symlink() or plistlib.loads(plist.read_bytes()).get("ProgramArguments", [None])[0] != program:
            raise RuntimeError("service file changed before unlink")
        ACTIONS.append({'action': 'unlink-plist', 'path': str(plist)})
        plist.unlink()
    staged = plist.with_name('.' + plist.name + '.install')
    if staged.exists() or staged.is_symlink():
        raise RuntimeError('staging plist remains; ownership unproven, retained')
    if plist.exists() or plist.is_symlink() or not job_absent(domain, target):
        raise RuntimeError('job/plist absence not verified')


def clean_session(record, root):
    expected_fixture = root / 'runtime/terminal-fixture'
    if Path(record['fixture']) != expected_fixture:
        raise RuntimeError('unowned tmux fixture')
    session = record.get('tmux_id') or '=shiki-' + record['session_id']
    if not (session.startswith('=shiki-sess_') or (session.startswith('$') and session[1:].isdigit())):
        raise RuntimeError('invalid exact tmux session target')
    socket = f'shiki-{os.getuid()}'
    probe = run(['tmux', '-L', socket, 'list-panes', '-t', session,
                 '-F', '#{session_name}\t#{session_id}\t#{pane_id}\t#{pane_pid}\t#{pane_start_command}'])
    if probe.returncode != 0:
        inventory = run(['tmux', '-L', socket, 'list-sessions', '-F', '#{session_name}'])
        if inventory.returncode == 0 and 'shiki-' + record['session_id'] not in inventory.stdout.splitlines():
            return
        if 'no server running' in inventory.stderr or 'No such file or directory' in inventory.stderr:
            return
        raise RuntimeError('cannot establish owned session absence')
    rows = probe.stdout.splitlines()
    if len(rows) != 1:
        raise RuntimeError('owned session pane topology changed; refusing cleanup')
    fields = rows[0].split('\t', 4)
    if len(fields) != 5 or fields[0] != 'shiki-' + record['session_id']:
        raise RuntimeError('owned session name changed; refusing cleanup')
    if record.get('tmux_id') and fields[:4] != ['shiki-' + record['session_id'], record['tmux_id'], record['pane_id'], str(record['pid'])]:
        raise RuntimeError('owned pane identity changed; refusing cleanup')
    if str(expected_fixture) not in fields[4]:
        raise RuntimeError('owned pane start command changed; refusing cleanup')
    ACTIONS.append({'action': 'kill-session', 'target': fields[1], 'pane': record.get('pane_id')})
    result = run(['tmux', '-L', socket, 'kill-session', '-t', fields[1]])
    if result.returncode != 0:
        raise RuntimeError(f'owned tmux session cleanup failed: {result.stderr}')
    inventory = run(['tmux', '-L', socket, 'list-sessions', '-F', '#{session_name}'])
    if inventory.returncode == 0:
        if 'shiki-' + record['session_id'] in inventory.stdout.splitlines():
            raise RuntimeError('owned session remains after cleanup')
    elif 'no server running' not in inventory.stderr and 'No such file or directory' not in inventory.stderr:
        raise RuntimeError('owned session absence not established')


def guard(root, inventory, signal=None):
    args = [str(root / 'processguard'), '-inventory', str(inventory)]
    if signal:
        args += ['-signal', signal]
    result = run(args)
    if result.returncode != 0:
        raise RuntimeError('process identity guard failed: ' + result.stderr)
    observations = json.loads(result.stdout)['processes']
    for item in observations:
        if item.get('signaled'):
            ACTIONS.append({'action': 'signal', 'signal': signal, 'identity': item.get('identity')})
    if any(item['state'] not in ('absent', 'present') for item in observations):
        raise RuntimeError('process identity changed/unknown; refusing cleanup: ' + result.stdout)
    return observations


def main():
    root = Path(sys.argv[1])
    if root.parent != Path('/private/tmp') or not root.name.startswith('shiki-schema-macos.'):
        raise RuntimeError('unexpected cleanup root')
    errors = []
    try:
        clean_job(root)
    except Exception as error:
        errors.append(str(error))
    metadata = root / 'runtime/owned.json'
    if not metadata.exists():
        if errors:
            raise RuntimeError('; '.join(errors))
        return
    if metadata.is_symlink() or metadata.stat().st_size > 1 << 20:
        raise RuntimeError('unsafe ownership metadata; ' + '; '.join(errors))
    value = json.loads(metadata.read_text())
    for session in value.get('sessions', []):
        try:
            clean_session(session, root)
        except Exception as error:
            errors.append(str(error))
    records = {record['pid']: record for record in value.get('processes', [])
               if Path(record['executable']).is_relative_to(root / 'runtime')
               and Path(record['executable']).name in ('shiki-daemon', 'shiki-agent', 'shiki-plugin-sync', 'shiki-plugin-port')}
    # A mismatched process must not prevent cleanup of independent owned peers.
    for record in records.values():
        try:
            inventory = root / 'runtime/cleanup-processes.json'
            with inventory.open('w') as stream:
                json.dump({'processes': [record]}, stream)
            inventory.chmod(0o600)
            for signal in ('TERM', 'KILL'):
                if all(item['state'] == 'absent' for item in guard(root, inventory)):
                    break
                print(f'Schema fallback: exact birth/executable/UID-verified {signal} PID={record["pid"]}')
                guard(root, inventory, signal)
                deadline = time.monotonic() + 5
                while time.monotonic() < deadline:
                    if all(item['state'] == 'absent' for item in guard(root, inventory)):
                        break
                    time.sleep(0.1)
            if any(item['state'] != 'absent' for item in guard(root, inventory)):
                raise RuntimeError('owned process remains; retain fixture for inspection')
        except Exception as error:
            errors.append(str(error))
    if errors:
        raise RuntimeError('; '.join(errors))
    print('Schema cleanup: owned job/plist/daemon/session absent; shared tmux server preserved')


if __name__ == '__main__':
    failure = None
    try:
        main()
    except Exception as error:
        failure = str(error)
        raise
    finally:
        root = Path(sys.argv[1])
        if root.parent == Path('/private/tmp') and root.name.startswith('shiki-schema-macos.') and root.is_dir():
            output = root / 'cleanup-result.json'
            previous = json.loads(output.read_text()) if output.exists() else {}
            result = {'fallback_actions': previous.get('fallback_actions', []) + ACTIONS,
                      'errors': previous.get('errors', []) + ([failure] if failure else [])}
            temporary = output.with_suffix('.tmp')
            temporary.write_text(json.dumps(result, indent=2) + '\n')
            temporary.chmod(0o600)
            temporary.replace(output)
