"""Real UID1000 agent/CLI observations against a private systemd user manager."""
import json
import os
from pathlib import Path
import sys
import time

sys.dont_write_bytecode = True
from common import digest, process_identity, run, wait_for

HOME = Path('/home/shikitest')
EVIDENCE = Path('/evidence')
CONFIG = HOME / '.shiki/config.yaml'
PREFIX = HOME / 'startup-install'
UNIT = HOME / '.config/systemd/user/shiki-agent.service'
ENV = {**os.environ, 'HOME': str(HOME), 'PATH': '/usr/local/go/bin:/usr/bin:/bin',
       'TMPDIR': '/build/tmp', 'GOCACHE': '/build/cache', 'GOPATH': '/go', 'GOTOOLCHAIN': 'local',
       'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOMAXPROCS': '2',
       'GOFLAGS': '-p=1 -mod=readonly -buildvcs=false', 'PYTHONDONTWRITEBYTECODE': '1'}
RECORD = {'started': time.time(), 'commands': [], 'observations': [], 'passed': False}


def save():
    tmp = EVIDENCE / 'scenario.tmp'
    tmp.write_text(json.dumps(RECORD, indent=2) + '\n')
    tmp.replace(EVIDENCE / 'scenario.json')


def command(args, **kwargs):
    start = time.time()
    result = run(args, check=False, **kwargs)
    RECORD['commands'].append({'args': [str(x) for x in args], 'started': start,
                               'finished': time.time(), 'exit': result.returncode,
                               'stdout': result.stdout, 'stderr': result.stderr})
    save()
    if result.returncode:
        raise RuntimeError(f'command exit {result.returncode}: {args}: {result.stderr}')
    return result


def user(args, **kwargs):
    return command(['/usr/sbin/runuser', '-u', 'shikitest', '--', 'env',
                    'HOME=' + str(HOME), 'USER=shikitest',
                    'XDG_RUNTIME_DIR=/run/user/1000', *args], env=ENV, **kwargs)


def write(path, data):
    user(['mkdir', '-p', '-m', '700', path.parent])
    user(['python3', '-c',
          'import os,sys; fd=os.open(sys.argv[1],os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600); '
          'f=os.fdopen(fd,"w"); f.write(sys.stdin.read()); f.flush(); os.fsync(f.fileno()); f.close()',
          path], input=data)


def ctl(*args):
    return user(['/usr/bin/systemctl', '--user', *args])


def properties():
    keys = 'LoadState,UnitFileState,FragmentPath,DropInPaths,NeedDaemonReload,Transient,MainPID,InvocationID,ActiveState,SubState'
    raw = ctl('show', 'shiki-agent.service', '--no-pager', '--property=' + keys).stdout
    return dict(line.split('=', 1) for line in raw.splitlines())


def observe(name, expected, original=None):
    before = properties()
    linger = user(['/usr/bin/loginctl', 'show-user', '1000', '--property=Linger', '--value', '--no-pager']).stdout.strip()
    unit = UNIT.read_bytes()
    config_hash = digest(CONFIG)
    link = UNIT.parent / 'default.target.wants/shiki-agent.service'
    target = os.readlink(link) if link.is_symlink() else None
    start = time.time()
    response = user([PREFIX / 'current/shiki', '--config', CONFIG, '--json', '--timeout', '10s', 'config', 'startup'], timeout=15)
    value = json.loads(response.stdout)
    after = properties()
    assert before == after and unit == UNIT.read_bytes() and config_hash == digest(CONFIG), 'query changed or raced service/config state'
    assert before['ActiveState'] == 'active' and int(before['MainPID']) > 1
    identity = process_identity(int(before['MainPID']))
    assert identity['uids'] == [1000] * 4
    assert identity['executable'] == str(PREFIX / 'current/shiki-agent')
    assert digest(identity['executable']) == RECORD['binary_hashes']['shiki-agent']
    if original:
        assert identity == original, 'query/control unexpectedly replaced current agent'
    assert value['config_path'] == str(CONFIG) and value['cli_auto_start'] == 'disabled'
    assert value['manager'] == 'systemd-user'
    assert start * 1000 - 1000 <= int(value['observed_at']) <= time.time() * 1000 + 1000
    for key, want in expected.items():
        assert value[key] == want, (name, key, value)
    entry = {'name': name, 'os_before': before, 'os_after': after, 'linger': linger,
             'enable_link': target, 'unit_sha256': digest(UNIT), 'agent_identity': identity,
             'response': value, 'elapsed': time.time() - start}
    RECORD['observations'].append(entry)
    (EVIDENCE / (name + '.unit')).write_bytes(unit)
    save()
    return identity


def main():
    assert os.geteuid() == 0 and Path('/proc/1/comm').read_text().strip() == 'systemd'
    EVIDENCE.mkdir(mode=0o700)
    HOME.chmod(0o700)
    os.chown(HOME, 1000, 1000)
    for path in [Path('/build'), Path('/build/cache'), Path('/build/tmp')]:
        path.mkdir(mode=0o700, exist_ok=True)
        os.chown(path, 1000, 1000)
    save()
    try:
        RECORD['systemd_version'] = command(['/usr/bin/systemctl', '--version']).stdout
        RECORD['temporary_mounts'] = {str(path): command(['findmnt', '-T', path, '-o', 'TARGET,OPTIONS']).stdout for path in ['/tmp', '/build/tmp']}
        RECORD['go_version'] = user(['go', 'version']).stdout.strip()
        packages = ['./internal/agent', './internal/client', './cmd/shiki', './test/architecture']
        for name, args in [('package-race', ['go', 'test', '-race', '-json', '-count=1', '-timeout=10m', *packages]),
                           ('package-vet', ['go', 'vet', *packages])]:
            result = user(args, cwd='/source', timeout=900)
            (EVIDENCE / (name + '.log')).write_text(result.stdout + result.stderr)
            if name == 'package-race':
                rows = [json.loads(line) for line in result.stdout.splitlines()]
                RECORD['package_results'] = [x for x in rows if x['Action'] in ('pass', 'fail', 'skip') and 'Test' not in x]
                RECORD['package_skips'] = [x for x in rows if x['Action'] == 'skip']
                assert len(RECORD['package_results']) == 4 and all(x['Action'] == 'pass' for x in RECORD['package_results'])
                assert not RECORD['package_skips'], 'selected package emitted skip; preserve as incomplete evidence'
            save()
        user(['mkdir', '-p', '-m', '700', PREFIX / 'current'])
        for name, package in [('shiki', './cmd/shiki'), ('shiki-agent', './cmd/shiki-agent'),
                              ('render', './test/diagnostics/startuppolicy')]:
            user(['go', 'build', '-o', PREFIX / 'current' / name, package], cwd='/source', timeout=900)
        RECORD['binary_hashes'] = {p.name: digest(p) for p in (PREFIX / 'current').iterdir()}
        write(CONFIG, 'schema_version: 1\nplugins_dir: ~/.shiki/plugins\nagent:\n  socket: ~/.shiki/run/agent.sock\n  state_dir: ~/.shiki/agent\n  auto_start: false\ntailscale:\n  enabled: false\n  hostname: startup-policy-test\n  state_dir: ~/.shiki/tailscale\n  control_url: https://controlplane.tailscale.com\nservers: {}\nprojects: {}\n')
        rendered = user([PREFIX / 'current/render', '--config', CONFIG, '--prefix', PREFIX]).stdout
        write(UNIT, rendered)
        command(['loginctl', 'enable-linger', 'shikitest'])
        command(['systemctl', 'start', 'user@1000.service'])
        wait_for(lambda: Path('/run/user/1000/bus').exists())
        ctl('daemon-reload')
        ctl('enable', '--now', 'shiki-agent.service')
        wait_for(lambda: (HOME / '.shiki/run/agent.sock').exists())
        original = observe('enabled-linger', {'managed_service': 'present', 'login_start': 'enabled', 'boot_start': 'enabled', 'reason_code': 'STARTUP_CONFIGURED_POLICY_OBSERVED'})
        assert RECORD['observations'][-1]['linger'] == 'yes'
        assert RECORD['observations'][-1]['os_before']['UnitFileState'] == 'enabled'
        ctl('disable', 'shiki-agent.service')
        observe('disabled-running', {'managed_service': 'present', 'login_start': 'disabled', 'boot_start': 'disabled', 'reason_code': 'STARTUP_MANAGED_SERVICE_DISABLED'}, original)
        assert RECORD['observations'][-1]['os_before']['UnitFileState'] == 'disabled'
        assert RECORD['observations'][-1]['enable_link'] is None
        ctl('enable', 'shiki-agent.service')
        wrong = user([PREFIX / 'current/render', '--config', HOME / '.shiki/other.yaml', '--prefix', PREFIX]).stdout
        write(UNIT, wrong)
        ctl('daemon-reload')
        observe('different-config', {'managed_service': 'unknown', 'login_start': 'unknown', 'boot_start': 'unknown', 'reason_code': 'STARTUP_SERVICE_CONFIGURATION_UNKNOWN'}, original)
        assert RECORD['observations'][-1]['os_before']['UnitFileState'] == 'enabled'
        write(UNIT, rendered)
        ctl('daemon-reload')
        observe('restored-binding', {'managed_service': 'present', 'login_start': 'enabled', 'boot_start': 'enabled', 'reason_code': 'STARTUP_CONFIGURED_POLICY_OBSERVED'}, original)
        ctl('stop', 'shiki-agent.service')
        wait_for(lambda: not Path('/proc', str(original['pid'])).exists())
        RECORD['agent_reaped'] = True
        RECORD['stopped_properties'] = properties()
        assert RECORD['stopped_properties']['ActiveState'] == 'inactive'
        assert RECORD['stopped_properties']['MainPID'] == '0'
        assert digest(PREFIX / 'current/shiki-agent') == RECORD['binary_hashes']['shiki-agent']
        RECORD['passed'] = True
    except BaseException as error:
        RECORD['error'] = repr(error)
        raise
    finally:
        RECORD['finished'] = time.time()
        save()


if __name__ == '__main__':
    main()
