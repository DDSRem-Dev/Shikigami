#!/usr/bin/env python3
"""One private real-systemd startup-policy scenario, never the install matrix."""
import argparse
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import traceback
import uuid

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parent.parent / 'serviceboot'))
from common import digest, inventory, run, wait_for, write_json

LABEL = 'dev.shikigami.startup-policy'


def main(args):
    repo = Path(__file__).resolve().parents[3]
    output = Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix='shiki-startup-policy-', dir='/tmp'))
    if output == repo or repo in output.parents:
        raise ValueError('evidence must be outside repository')
    output.mkdir(mode=0o700, parents=True, exist_ok=True)
    output.chmod(0o700)
    if any(output.iterdir()):
        raise ValueError('evidence directory must be empty')
    print('Evidence: ' + str(output), flush=True)
    expected = json.loads(Path(args.manifest).read_text())
    assert digest(args.manifest) == args.manifest_sha256
    assert inventory(args.source) == expected
    source, tools = output / 'source', output / 'tools'
    shutil.copytree(args.source, source)
    shutil.copytree(Path(__file__).parent, tools, ignore=shutil.ignore_patterns('__pycache__'))
    # copytree preserves a frozen directory's 0555 mode. Only the newly owned
    # tool directory needs owner access while adding the two shared helpers.
    tools.chmod(0o700)
    for filename in ['common.py', 'Dockerfile']:
        shutil.copy2(repo / 'test/diagnostics/serviceboot' / filename, tools / filename)
    shutil.copy2(repo / 'scripts/test-startup-policy.sh', tools / 'wrapper.sh')
    tool_hashes = inventory(tools)
    assert inventory(source) == expected
    for root in [source, tools]:
        for p in [root, *root.rglob('*')]:
            p.chmod(0o555 if p.is_dir() or p.stat().st_mode & 0o111 else 0o444)
    write_json(output / 'source-sha256.json', expected)
    write_json(output / 'tool-sha256.json', tool_hashes)
    info = json.loads(run(['docker', 'info', '--format', '{{json .}}']).stdout)
    assert info['OSType'] == 'linux' and info['CgroupVersion'] == '2'
    modules = Path(run(['go', 'env', 'GOMODCACHE'], cwd=source).stdout.strip()).resolve()
    name = 'shiki-startup-policy-' + uuid.uuid4().hex[:12]
    image, cid = name + ':local', None
    evidence = {'started': time.time(), 'source_files': len(expected),
                'source_manifest_sha256': digest(output / 'source-sha256.json'),
                'tools_manifest_sha256': digest(output / 'tool-sha256.json'),
                'container_name': name, 'forced_cleanup': False, 'acceptance': 'failed',
                'commands': [], 'errors': []}

    def call(argv, **kwargs):
        started = time.time()
        result = run(argv, check=False, **kwargs)
        evidence['commands'].append({'args': [str(x) for x in argv], 'started': started,
                                     'finished': time.time(), 'exit': result.returncode,
                                     'stdout': result.stdout, 'stderr': result.stderr})
        if result.returncode:
            raise RuntimeError(f'command failed: {argv}: {result.stderr}')
        return result

    try:
        build = call(['docker', 'build', '--platform', 'linux/arm64', '-t', image, tools], timeout=600)
        (output / 'image-build.log').write_text(build.stdout + build.stderr)
        cid = call(['docker', 'create', '--platform', 'linux/arm64', '--name', name,
                    '--label', LABEL + '=' + name, '--cgroupns', 'private', '--network', 'none',
                    '--cap-add', 'SYS_ADMIN', '--tmpfs', '/run', '--tmpfs', '/run/lock', '--tmpfs', '/tmp',
                    '--mount', f'type=bind,source={source},target=/source,readonly',
                    '--mount', f'type=bind,source={tools},target=/tools,readonly',
                    '--mount', f'type=bind,source={modules},target=/go/pkg/mod,readonly', image,
                    '/bin/bash', '-c', 'set -euo pipefail; mount -o remount,rw /sys/fs/cgroup; exec /sbin/init --log-target=console --log-level=info']).stdout.strip()
        (output / 'container.cid').write_text(cid + '\n')
        call(['docker', 'start', cid])
        wait_for(lambda: run(['docker', 'exec', cid, 'systemctl', 'is-system-running'], check=False).stdout.strip() == 'running', timeout=60)
        inspected = json.loads(call(['docker', 'inspect', cid]).stdout)[0]
        host = inspected['HostConfig']
        assert inspected['Config']['Labels'][LABEL] == name
        assert not host['Privileged'] and host['CgroupnsMode'] == 'private'
        assert host['PidMode'] != 'host' and host['IpcMode'] != 'host'
        assert host['NetworkMode'] == 'none' and not host['PortBindings']
        assert [x.removeprefix('CAP_') for x in host['CapAdd']] == ['SYS_ADMIN']
        assert not host['SecurityOpt'] and len(inspected['Mounts']) == 3
        assert all(x['Type'] == 'bind' and not x['RW'] for x in inspected['Mounts'])
        write_json(output / 'container-started.json', inspected)
        print('Real systemd ready: package regressions then startup-policy scenario', flush=True)
        with (output / 'scenario.log').open('x') as log:
            process = subprocess.run(['docker', 'exec', cid, 'python3', '-B', '/tools/scenario.py'],
                                     stdout=log, stderr=subprocess.STDOUT, timeout=1800)
        evidence['scenario_exit'] = process.returncode
        call(['docker', 'cp', cid + ':/evidence/.', output / 'container-evidence'])
        if process.returncode:
            raise RuntimeError('scenario failed; see retained logs')
        scenario = json.loads((output / 'container-evidence/scenario.json').read_text())
        assert scenario['passed'] and scenario['agent_reaped']
        call(['docker', 'exec', cid, 'systemctl', 'poweroff'])
        result = call(['docker', 'wait', cid], timeout=60)
        state = json.loads(call(['docker', 'inspect', '--format', '{{json .State}}', cid]).stdout)
        assert result.stdout.strip() == '0' and not state['Running'] and state['Pid'] == 0 and not state['OOMKilled']
        write_json(output / 'normal-shutdown.json', state)
        evidence['normal_poweroff_exit'] = 0
        evidence['acceptance'] = 'passed'
    except BaseException as error:
        evidence['errors'].append(repr(error))
        raise
    finally:
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        try:
            found = run(['docker', 'inspect', cid or name], check=False)
            if found.returncode == 0:
                inspected = json.loads(found.stdout)[0]
                assert inspected['Config']['Labels'].get(LABEL) == name
                cid = inspected['Id']
                logs = run(['docker', 'logs', cid], check=False)
                (output / 'systemd.log').write_text(logs.stdout + logs.stderr)
                run(['docker', 'cp', cid + ':/evidence/.', output / 'container-evidence'], check=False)
                if inspected['State']['Running']:
                    evidence['forced_cleanup'] = True
                    journal = run(['docker', 'exec', cid, 'journalctl', '--no-pager'], check=False)
                    (output / 'failure-journal.log').write_text(journal.stdout + journal.stderr)
                    run(['docker', 'exec', cid, 'systemctl', 'poweroff'], check=False)
                    try:
                        run(['docker', 'wait', cid], timeout=30)
                    except subprocess.TimeoutExpired:
                        run(['docker', 'stop', '--time', '20', cid], check=False, timeout=30)
                write_json(output / 'cleanup-state.json', json.loads(run(['docker', 'inspect', '--format', '{{json .State}}', cid]).stdout))
                call(['docker', 'rm', '-f', cid])
            absent = run(['docker', 'inspect', cid or name], check=False)
            ids = run(['docker', 'ps', '-a', '--no-trunc', '--format', '{{.ID}}']).stdout.splitlines()
            evidence['container_absence'] = {'exit': absent.returncode, 'stderr': absent.stderr, 'inventory': ids}
            evidence['container_absent'] = absent.returncode != 0 and 'no such object' in absent.stderr.lower() and cid not in ids
            run(['docker', 'image', 'rm', image], check=False)
        except BaseException as error:
            evidence['errors'].append('cleanup: ' + repr(error))
        finally:
            evidence['source_unchanged'] = inventory(source) == expected
            evidence['tools_unchanged'] = inventory(tools) == tool_hashes
            evidence['container_id'] = cid
            evidence['finished'] = time.time()
            write_json(output / 'evidence.json', evidence)
    assert evidence['acceptance'] == 'passed' and not evidence['forced_cleanup'] and not evidence['errors']
    assert all(evidence[x] for x in ['source_unchanged', 'tools_unchanged', 'container_absent'])
    print('PASS: ' + str(output / 'evidence.json'), flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output')
    parser.add_argument('--source', required=True)
    parser.add_argument('--manifest', required=True)
    parser.add_argument('--manifest-sha256', required=True)
    args = parser.parse_args()
    def interrupt(signum, frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGINT, interrupt)
    signal.signal(signal.SIGTERM, interrupt)
    try:
        main(args)
    except KeyboardInterrupt:
        sys.exit(130)
    except Exception:
        traceback.print_exc()
        sys.exit(1)
