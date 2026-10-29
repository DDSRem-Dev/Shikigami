#!/usr/bin/env python3
"""Freeze and run one native or isolated Linux ordinary-build experiment."""
import argparse
import ctypes
import errno
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import uuid

sys.dont_write_bytecode = True
from common import digest, inventory, run, write_json

LABEL = 'dev.shikigami.load-capacity'


class BSDInfo(ctypes.Structure):
    # Darwin proc_bsdinfo (PROC_PIDTBSDINFO), including microsecond birth identity.
    _fields_ = [(name, ctypes.c_uint32) for name in (
        'flags', 'status', 'xstatus', 'pid', 'ppid', 'uid', 'gid', 'ruid', 'rgid',
        'svuid', 'svgid', 'rfu')]
    _fields_ += [('comm', ctypes.c_char * 16), ('name', ctypes.c_char * 32)]
    _fields_ += [(name, ctypes.c_uint32) for name in ('nfiles', 'pgid', 'pjobc', 'e_tdev', 'e_tpgid')]
    _fields_ += [('nice', ctypes.c_int32), ('start_sec', ctypes.c_uint64), ('start_usec', ctypes.c_uint64)]


def native_identity(pid):
    lib = ctypes.CDLL('/usr/lib/libproc.dylib', use_errno=True)
    lib.proc_pidinfo.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_uint64, ctypes.c_void_p, ctypes.c_int]
    lib.proc_pidinfo.restype = ctypes.c_int
    lib.proc_pidpath.argtypes = [ctypes.c_int, ctypes.c_void_p, ctypes.c_uint32]
    lib.proc_pidpath.restype = ctypes.c_int

    def birth():
        value = BSDInfo()
        ctypes.set_errno(0)
        count = lib.proc_pidinfo(pid, 3, 0, ctypes.byref(value), ctypes.sizeof(value))
        if count != ctypes.sizeof(value):
            code = ctypes.get_errno()
            if code == errno.ESRCH:
                return None
            raise OSError(code, 'cannot inspect exact native process birth')
        assert value.pid == pid
        return value

    before = birth()
    if before is None:
        return None
    path = ctypes.create_string_buffer(4096)
    ctypes.set_errno(0)
    if lib.proc_pidpath(pid, path, len(path)) <= 0:
        code = ctypes.get_errno()
        if code == errno.ESRCH and birth() is None:
            return None
        raise OSError(code, 'cannot inspect exact native executable')
    executable = os.fsdecode(path.value)
    sha = digest(executable)
    after = birth()
    if after is None:
        return None
    if (before.start_sec, before.start_usec) != (after.start_sec, after.start_usec):
        raise RuntimeError('process generation changed during native inspection')
    assert (before.uid, before.ruid, before.svuid) == (after.uid, after.ruid, after.svuid)
    return {'PID': pid, 'Created': before.start_sec * 1000 + before.start_usec // 1000,
            'Executable': executable, 'UID': before.uid, 'SHA256': sha,
            'UIDs': [before.uid, before.ruid, before.svuid]}


def owned_native(prior):
    assert int(prior['PID']) > 1 and int(prior['PID']) != os.getpid()
    current = native_identity(int(prior['PID']))
    if current is None or current['Created'] != prior['Created']:
        return None
    assert current['UIDs'] == [os.getuid()] * 3 and prior['UID'] == os.getuid()
    for field in ('PID', 'Created', 'Executable', 'UID', 'SHA256'):
        assert current[field] == prior[field], 'owned process identity mismatch: ' + field
    return current


def session_present(runtime):
    socket, name = runtime['tmux_socket'], runtime['tmux_session']
    assert socket == 'shiki-' + str(os.getuid()) and name.startswith('shiki-sess_')
    result = run(['tmux', '-L', socket, 'list-sessions', '-F', '#{session_id}\t#{session_name}\t#{session_created}'], check=False)
    if result.returncode:
        if 'no server running' in result.stderr or 'no sessions' in result.stderr or 'No such file or directory' in result.stderr:
            return None
        raise RuntimeError('tmux absence could not be verified: ' + result.stderr)
    matches = [line.split('\t') for line in result.stdout.splitlines() if line.split('\t')[1] == name]
    if not matches:
        return None
    assert len(matches) == 1
    found = matches[0]
    assert found[0] == runtime['tmux_session_id'] and int(found[2]) == int(runtime['tmux_created'])
    assert found[0].startswith('$') and found[0][1:].isdigit()
    panes = run(['tmux', '-L', socket, 'list-panes', '-t', found[0],
                 '-F', '#{pane_pid}\t#{pane_start_command}']).stdout.splitlines()
    assert panes == [str(runtime['tmux_pane_pid']) + '\t' + runtime['tmux_pane_start_command']]
    return found[0]


def native_cleanup(output, record, attempt):
    evidence = output / 'run'
    if not (evidence / 'command.json').exists():
        record['native_application_started'] = False
        return
    record['native_application_started'] = True
    ledger = json.loads((evidence / 'owned-processes.json').read_text())
    runtime = json.loads((evidence / 'runtime.json').read_text())
    private = Path(runtime['private_root'])
    assert private.is_absolute() and private.name.startswith('shiki-capacity-private-')
    assert private.parent.resolve() == Path('/tmp').resolve()
    if private.exists():
        assert private.is_dir() and not private.is_symlink() and private.stat().st_uid == os.getuid()
        assert private.stat().st_mode & 0o077 == 0
        record['retained_private_runtime'] = str(private)
    outcomes = []
    record['native_process_cleanup'] = outcomes
    # Terminate only complete, fresh, individually verified ownership records.
    for prior in ledger:
        def inspect_and_stop(prior=prior):
            outcome = {'identity': prior, 'forced': False}
            outcomes.append(outcome)
            current = owned_native(prior)
            if current is not None:
                record['forced_cleanup'] = outcome['forced'] = True
                os.kill(prior['PID'], signal.SIGTERM)
            outcome['initially_absent'] = current is None
        attempt('native process termination', inspect_and_stop)
    if runtime.get('tmux_session'):
        def stop_session():
            found = session_present(runtime)
            if found is not None:
                record['forced_cleanup'] = True
                # Recheck immutable session ID/birth immediately before exact operation.
                assert session_present(runtime) == found
                run(['tmux', '-L', runtime['tmux_socket'], 'kill-session', '-t', found])
        attempt('native session cleanup', stop_session)
    deadline = time.monotonic() + 15
    remaining = list(ledger)
    unknown = []
    while remaining and time.monotonic() < deadline:
        next_remaining = []
        for prior in remaining:
            try:
                if owned_native(prior) is not None:
                    next_remaining.append(prior)
            except BaseException as error:
                record['errors'].append('native cleanup inspection: ' + repr(error))
                unknown.append(prior)
        remaining = next_remaining
        if remaining:
            time.sleep(.1)
    for prior in remaining:
        def force(prior=prior):
            if owned_native(prior) is not None:
                record['forced_cleanup'] = True
                os.kill(prior['PID'], signal.SIGKILL)
        attempt('native last-resort owned kill', force)
    deadline = time.monotonic() + 5
    while remaining and time.monotonic() < deadline:
        remaining = [prior for prior in remaining if owned_native(prior) is not None]
        if remaining:
            time.sleep(.1)
    record['native_processes_absent'] = not remaining and not unknown
    if runtime.get('tmux_session'):
        record['native_session_absent'] = session_present(runtime) is None
        assert record['native_session_absent']
    else:
        record['native_session_absent'] = True
    assert record['native_processes_absent'], 'owned native processes remain or inspection is unknown'
    # Never erase a failed private runtime during fallback.
    if record.get('passed') and private.exists():
        raise RuntimeError('successful driver retained its private runtime')


def main(args):
    repo = Path(__file__).resolve().parents[3]
    output = Path(tempfile.mkdtemp(prefix='shiki-load-' + args.platform + '-', dir='/tmp')).resolve()
    os.chmod(output, 0o700)
    print('Evidence: ' + str(output), flush=True)
    source, tools = output / 'source', output / 'tools'
    record = {'platform': args.platform, 'mode': 'harness-precheck' if args.precheck else 'admission-route-diagnostic' if args.admission_probe else 'formal',
              'started': time.time(), 'forced_cleanup': False, 'passed': False, 'errors': []}
    cid = image = name = child = expected = tool_hashes = None
    interrupted = []
    create_attempted = False

    def attempt(label, operation):
        try:
            return operation()
        except BaseException as error:
            record['errors'].append(label + ': ' + type(error).__name__ + ': ' + str(error))
            return None

    def interrupt(signum, frame):
        interrupted.append(signum)
        raise InterruptedError('runner interrupted by signal ' + str(signum))

    def inspect_owned_container():
        found = json.loads(run(['docker', 'inspect', cid]).stdout)[0]
        assert found['Id'] == cid and found['Config']['Labels'].get(LABEL) == name
        return found

    signal.signal(signal.SIGINT, interrupt)
    signal.signal(signal.SIGTERM, interrupt)
    try:
        manifest = Path(args.manifest)
        assert digest(manifest) == args.manifest_sha256
        expected = json.loads(manifest.read_text())
        assert inventory(args.source) == expected
        shutil.copytree(args.source, source)
        shutil.copytree(Path(__file__).parent, tools, ignore=shutil.ignore_patterns('__pycache__'))
        # A frozen launcher directory is read-only; this new private copy must
        # admit the executable wrapper before the whole tree is frozen.
        os.chmod(tools, 0o700)
        shutil.copy2(repo / 'scripts/test-load-capacity.sh', tools / 'wrapper.sh')
        tool_hashes = inventory(tools)
        write_json(output / 'source-sha256.json', expected)
        write_json(output / 'tools-sha256.json', tool_hashes)
        for root in (source, tools):
            for path in [root, *root.rglob('*')]:
                os.chmod(path, 0o555 if path.is_dir() or path.stat().st_mode & 0o111 else 0o444)
        write_json(output / 'frozen-modes.json', {str(p.relative_to(output)): oct(p.stat().st_mode & 0o777)
                   for root in (source, tools) for p in [root, *root.rglob('*')]})
        record.update(source_manifest_sha256=digest(output / 'source-sha256.json'),
                      tools_manifest_sha256=digest(output / 'tools-sha256.json'), source_count=len(expected))
        if args.platform == 'native':
            assert platform.system() == 'Darwin' and os.getuid() != 0
            goroot = run(['go', 'env', 'GOROOT']).stdout.strip()
            env = {**os.environ, 'PATH': goroot + '/bin:' + os.environ['PATH']}
            command = ['python3', '-B', str(tools / 'build.py'), '--source', str(source), '--tools', str(tools),
                       '--work', str(output / 'work'), '--output', str(output / 'run')]
            if args.precheck:
                command += ['--precheck']
            elif args.admission_probe:
                command += ['--admission-probe']
            with (output / 'runner.log').open('x') as log:
                child = subprocess.Popen(command, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                record['exit'] = child.wait(timeout=7200)
            assert record['exit'] == 0, 'build/driver exited ' + str(record['exit']) + '; see run/driver-exit.json and run/result.json'
        else:
            modules = Path(run(['go', 'env', 'GOMODCACHE'], cwd=source).stdout.strip()).resolve()
            name = 'shiki-load-capacity-' + uuid.uuid4().hex[:12]
            image = name + ':local'
            built = run(['docker', 'build', '--platform', 'linux/arm64', '-t', image, tools], timeout=600)
            (output / 'image-build.log').write_text(built.stdout + built.stderr)
            command = ['python3', '-B', '/tools/build.py', '--source', '/source', '--tools', '/tools',
                       '--work', '/home/shikitest/work', '--output', '/home/shikitest/evidence']
            if args.precheck:
                command += ['--precheck']
            elif args.admission_probe:
                command += ['--admission-probe']
            create_attempted = True
            cid = run(['docker', 'create', '--cidfile', output / 'container.cid', '--init', '--platform', 'linux/arm64', '--name', name,
                       '--label', LABEL + '=' + name, '--network', 'none', '--user', '1000:1000',
                       '--mount', f'type=bind,source={source},target=/source,readonly',
                       '--mount', f'type=bind,source={tools},target=/tools,readonly',
                       '--mount', f'type=bind,source={modules},target=/go/pkg/mod,readonly', image, *command]).stdout.strip()
            assert len(cid) == 64 and all(c in '0123456789abcdef' for c in cid)
            assert (output / 'container.cid').read_text().strip() == cid
            record['container_id'] = cid
            inspected = inspect_owned_container()
            host = inspected['HostConfig']
            assert inspected['Config']['User'] == '1000:1000' and not host['Privileged'] and not host['CapAdd'] and not host['SecurityOpt']
            assert host['Init'] and host['NetworkMode'] == 'none' and host['PidMode'] != 'host' and host['IpcMode'] != 'host' and not host['PortBindings']
            assert len(inspected['Mounts']) == 3 and all(m['Type'] == 'bind' and not m['RW'] for m in inspected['Mounts'])
            write_json(output / 'container-inspect.json', inspected)
            run(['docker', 'start', cid])
            with (output / 'container-wait.log').open('x') as log:
                child = subprocess.Popen(['docker', 'wait', cid], stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                assert child.wait(timeout=7200) == 0
            run(['docker', 'cp', cid + ':/home/shikitest/evidence/.', output / 'run'])
            state = inspect_owned_container()['State']
            write_json(output / 'container-exit.json', state)
            record['exit'] = state['ExitCode']
            assert state['ExitCode'] == 0 and not state['Running'] and state['Pid'] == 0 and not state['OOMKilled']
        result = json.loads((output / 'run/result.json').read_text())
        driver = json.loads((output / 'run/driver-exit.json').read_text())
        assert result['Passed'] and not result['Errors'] and driver['passed'] and not driver['forced_cleanup']
        record['passed'] = True
    except BaseException as error:
        record['errors'].append(type(error).__name__ + ': ' + str(error))
    finally:
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        if child is not None:
            def stop_child():
                if child.poll() is None:
                    record['forced_cleanup'] = True
                    # Unreaped child owns this newly created session/process group.
                    os.killpg(child.pid, signal.SIGTERM)
                    try:
                        child.wait(timeout=45)
                    except subprocess.TimeoutExpired:
                        os.killpg(child.pid, signal.SIGKILL)
                        child.wait(timeout=15)
                record['wrapper_child_exit'] = child.returncode
            attempt('wrapper child cleanup', stop_child)
        if args.platform == 'native':
            attempt('native owned cleanup', lambda: native_cleanup(output, record, attempt))
        if cid is None and create_attempted:
            def recover_created_cid():
                result = run(['docker', 'inspect', name], check=False)
                if result.returncode:
                    assert 'no such object' in result.stderr.lower()
                    record['container_absent'] = True
                    return None
                recovered = json.loads(result.stdout)[0]
                assert recovered['Config']['Labels'].get(LABEL) == name
                assert recovered['Name'] == '/' + name
                record['container_id'] = recovered['Id']
                return recovered['Id']
            cid = attempt('recover partial create identity', recover_created_cid)
        if cid is not None:
            found = attempt('container ownership', inspect_owned_container)
            if found is not None:
                def stop_container():
                    if inspect_owned_container()['State']['Running']:
                        record['forced_cleanup'] = True
                        run(['docker', 'stop', '--time', '30', cid], timeout=40)
                    final_state = inspect_owned_container()['State']
                    record['container_final_state'] = final_state
                    assert not final_state['Running'] and final_state['Pid'] == 0
                attempt('container stop', stop_container)
                def copy_evidence():
                    target = output / 'run'
                    target.mkdir(mode=0o700, exist_ok=True)
                    run(['docker', 'cp', cid + ':/home/shikitest/evidence/.', target])
                    return True
                evidence_saved = attempt('container evidence copy', copy_evidence)
                def retain_runtime():
                    runtime_file = output / 'run/runtime.json'
                    if not runtime_file.exists():
                        assert not (output / 'run/command.json').exists(), 'driver startup has no runtime receipt'
                        return True
                    private = json.loads(runtime_file.read_text())['private_root']
                    path = Path(private)
                    assert path.parent == Path('/tmp') and path.name.startswith('shiki-capacity-private-')
                    if not record.get('passed') or record['forced_cleanup'] or record['errors']:
                        copied = run(['docker', 'cp', cid + ':' + private, output / 'private-runtime'], check=False)
                        record['private_runtime_copy'] = {'exit': copied.returncode, 'stderr': copied.stderr}
                        # A normally deleted runtime is valid only with a successful cleanup receipt.
                        if copied.returncode:
                            cleanup = json.loads((output / 'run/cleanup.json').read_text())
                            assert cleanup['runtime_absent'], 'failed runtime could not be preserved'
                    return True
                runtime_saved = attempt('container private runtime retention', retain_runtime)
                def logs():
                    result = run(['docker', 'logs', cid], check=False)
                    (output / 'container.log').write_text(result.stdout + result.stderr)
                attempt('container logs', logs)
                def remove_container():
                    inspect_owned_container()
                    run(['docker', 'rm', '-f', cid])
                    absent = run(['docker', 'inspect', cid], check=False)
                    ids = run(['docker', 'ps', '-a', '--no-trunc', '--format', '{{.ID}}']).stdout.splitlines()
                    record['container_absent'] = (absent.returncode != 0 and 'no such object' in absent.stderr.lower() and cid not in ids)
                    assert record['container_absent']
                if (evidence_saved and runtime_saved) or found['State']['Status'] == 'created':
                    attempt('exact container removal', remove_container)
                else:
                    record['retained_container_id'] = cid
                    record['errors'].append('container retained because private failure evidence could not be copied')
        if image is not None:
            attempt('owned image removal', lambda: run(['docker', 'image', 'rm', image]))
        for key, root, wanted in [('source_unchanged', source, expected), ('tools_unchanged', tools, tool_hashes)]:
            def check(key=key, root=root, wanted=wanted):
                record[key] = wanted is not None and inventory(root) == wanted
                assert record[key]
            attempt(key, check)
        record['signals'] = interrupted
        record['finished'] = time.time()
        record['passed'] = record['passed'] and not record['errors'] and not record['forced_cleanup'] and not interrupted
        # Last action, after every independent cleanup/check has been attempted.
        write_json(output / 'evidence.json', record)
    if record['passed']:
        print('PASS: ' + str(output / 'evidence.json'), flush=True)
    else:
        print('FAIL: ' + str(output / 'evidence.json'), flush=True)
    return 0 if record['passed'] else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--platform', choices=('native', 'linux'), required=True)
    parser.add_argument('--source', required=True)
    parser.add_argument('--manifest', required=True)
    parser.add_argument('--manifest-sha256', required=True)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--precheck', action='store_true')
    mode.add_argument('--admission-probe', action='store_true')
    sys.exit(main(parser.parse_args()))
