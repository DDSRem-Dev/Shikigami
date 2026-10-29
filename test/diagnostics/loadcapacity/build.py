#!/usr/bin/env python3
"""One ordinary build and one driver invocation; no implicit retry."""
import argparse
import json
import os
from pathlib import Path
import shutil
import signal
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
from common import digest, inventory, run, write_json


def source_inventory(work):
    # Prune output/cache before traversal: hashing cached builds is not a source check.
    result = {}
    for base, directories, files in os.walk(work, followlinks=False):
        if Path(base) == work:
            directories[:] = [name for name in directories if name not in ('go-cache', 'capacity-bin')]
        for name in directories + files:
            path = Path(base) / name
            mode = path.lstat().st_mode
            if stat.S_ISLNK(mode) or not (stat.S_ISDIR(mode) or stat.S_ISREG(mode)):
                raise RuntimeError('source inventory refuses special file: ' + str(path))
            if stat.S_ISREG(mode):
                result[str(path.relative_to(work))] = digest(path)
    return result


def main(args):
    source, work, output, tools = map(Path, (args.source, args.work, args.output, args.tools))
    assert os.geteuid() != 0
    output.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(output, 0o700)
    record = {'started': time.time(), 'uid': os.getuid(), 'errors': [],
              'forced_cleanup': False, 'driver_started': False, 'passed': False}
    driver = None
    original = actual = artifacts = None
    interrupted = []

    def interrupt(signum, frame):
        interrupted.append(signum)
        if driver is not None and driver.poll() is None:
            driver.send_signal(signum)
        else:
            raise InterruptedError('build interrupted before driver startup')

    signal.signal(signal.SIGINT, interrupt)
    signal.signal(signal.SIGTERM, interrupt)
    try:
        original = inventory(source)
        shutil.copytree(source, work)
        for path in [work, *work.rglob('*')]:
            os.chmod(path, 0o700 if path.is_dir() or path.stat().st_mode & 0o111 else 0o600)
        assert not (work / 'go-cache').exists() and not (work / 'capacity-bin').exists()
        diagnostic = work / 'test/diagnostics/loadcapacity/workload'
        shutil.copytree(tools / 'workload', diagnostic)
        for name in ('diagnostic.mod', 'diagnostic.sum'):
            shutil.copy2(tools / name, diagnostic.parent / name)
        actual = source_inventory(work)
        expected = {**original, **{'test/diagnostics/loadcapacity/workload/' + name: value
                                  for name, value in inventory(tools / 'workload').items()}}
        expected.update({'test/diagnostics/loadcapacity/' + name: digest(tools / name)
                         for name in ('diagnostic.mod', 'diagnostic.sum')})
        assert actual == expected
        write_json(output / 'actual-work-source-sha256.json', actual)
        env = {**os.environ, 'GOMAXPROCS': '2', 'GOFLAGS': '-p=1 -mod=readonly -buildvcs=false',
               'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOTOOLCHAIN': 'local', 'GOCACHE': str(work / 'go-cache')}
        version = run(['go', 'version'], env=env).stdout.strip()
        assert 'go1.26.10' in version
        binary = work / 'capacity-bin'
        binary.mkdir(mode=0o700)
        build_log = output / 'build.log'
        with build_log.open('x') as log:
            os.chmod(build_log, 0o600)
            for name, target in [('shiki', './cmd/shiki'), ('shiki-agent', './cmd/shiki-agent'),
                                 ('shiki-daemon', './cmd/shiki-daemon'), ('shiki-plugin-sync', './plugins/sync'),
                                 ('shiki-plugin-port', './plugins/port'), ('capacity-driver', './test/diagnostics/loadcapacity/workload')]:
                diagnostic_flags = (['-tags=shiki_load_capacity',
                                     '-modfile=test/diagnostics/loadcapacity/diagnostic.mod']
                                    if name == 'capacity-driver' else [])
                subprocess.run(['go', 'build', *diagnostic_flags, '-trimpath', '-ldflags',
                                '-X github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo.Version=loadcapacity-local',
                                '-o', binary / name, target], cwd=work, env=env, stdout=log,
                               stderr=subprocess.STDOUT, check=True, timeout=900)
            for plugin in ('sync', 'port'):
                subprocess.run(['go', 'run', 'scripts/plugin-manifest.go', '--dir', binary, '--id', plugin,
                                '--version', 'loadcapacity-local'], cwd=work, env=env, stdout=log,
                               stderr=subprocess.STDOUT, check=True, timeout=120)
        artifacts = inventory(binary)
        buildinfo = {p.name: run(['go', 'version', '-m', p], env=env).stdout
                     for p in binary.iterdir() if not p.name.endswith('.json')}
        assert all('-race=true' not in value for value in buildinfo.values())
        assert 'github.com/charmbracelet/x/vt\tv0.1.0\t' in buildinfo['capacity-driver']
        assert all('github.com/charmbracelet/x/vt\t' not in value
                   for name, value in buildinfo.items() if name != 'capacity-driver')
        write_json(output / 'artifacts.json', {'go': version, 'files': artifacts, 'buildinfo': buildinfo,
                                              'gomaxprocs_build': 2, 'goflags': env['GOFLAGS'],
                                              'driver_only_modfile': 'test/diagnostics/loadcapacity/diagnostic.mod',
                                              'driver_only_build_tag': 'shiki_load_capacity'})
        assert inventory(source) == original and source_inventory(work) == actual
        args_run = [str(binary / 'capacity-driver'), '--bin', str(binary), '--output', str(output)]
        if args.precheck:
            args_run += ['--precheck']
        elif args.admission_probe:
            args_run += ['--admission-probe']
        write_json(output / 'command.json', {'command': args_run, 'uid': os.getuid(), 'started': time.time(),
                                            'mode': 'harness-precheck' if args.precheck else 'admission-route-diagnostic' if args.admission_probe else 'formal'})
        with (output / 'driver.log').open('x') as log:
            os.chmod(output / 'driver.log', 0o600)
            driver = subprocess.Popen(args_run, stdout=log, stderr=subprocess.STDOUT,
                                      env={**os.environ, 'GOMAXPROCS': '2'})
            record.update(driver_started=True, driver_pid=driver.pid)
            try:
                record['exit'] = driver.wait(timeout=1800)
            except subprocess.TimeoutExpired:
                record['timed_out'] = True
                raise RuntimeError('driver exceeded fixed overall experiment bound')
        assert record['exit'] == 0, 'driver exited ' + str(record['exit']) + '; see result.json and private driver.log'
        result = json.loads((output / 'result.json').read_text())
        assert result['Passed'] and not result['Errors']
        assert not interrupted
        record['passed'] = True
    except BaseException as error:
        record['errors'].append(type(error).__name__ + ': ' + str(error))
    finally:
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        if driver is not None:
            try:
                if driver.poll() is None:
                    record['forced_cleanup'] = True
                    driver.terminate()
                    try:
                        driver.wait(timeout=30)
                    except subprocess.TimeoutExpired:
                        record['driver_killed'] = True
                        driver.kill()
                        driver.wait(timeout=15)
                record['exit'] = driver.returncode
            except BaseException as error:
                record['errors'].append('driver cleanup: ' + repr(error))
        # Each independent check is attempted even after an earlier failure.
        for key, probe in [
            ('source_unchanged', lambda: original is not None and inventory(source) == original),
            ('actual_work_source_unchanged', lambda: actual is not None and source_inventory(work) == actual),
            ('artifacts_unchanged', lambda: artifacts is not None and inventory(work / 'capacity-bin') == artifacts),
        ]:
            try:
                record[key] = probe()
            except BaseException as error:
                record[key] = False
                record['errors'].append(key + ': ' + repr(error))
        record['signals'] = interrupted
        record['finished'] = time.time()
        record['passed'] = (record['passed'] and not record['errors'] and not record['forced_cleanup']
                            and not interrupted and all(record.get(k) for k in
                            ('source_unchanged', 'actual_work_source_unchanged', 'artifacts_unchanged')))
        write_json(output / 'driver-exit.json', record)
    return 0 if record['passed'] else 1


if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('--source', required=True)
    p.add_argument('--tools', required=True)
    p.add_argument('--work', required=True)
    p.add_argument('--output', required=True)
    mode = p.add_mutually_exclusive_group()
    mode.add_argument('--precheck', action='store_true')
    mode.add_argument('--admission-probe', action='store_true')
    sys.exit(main(p.parse_args()))
