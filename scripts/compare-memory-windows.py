#!/usr/bin/env python3
"""Compare one same-build load/control pair without a leak or capacity verdict."""

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path


def read(path):
    return json.loads(path.read_text())


def condition(root, mode):
    evidence = read(root / 'evidence.json')
    if (evidence['application_exit_code'] != 0 or evidence['collector_exit_code'] != 0
            or evidence['errors'] or evidence['changed_source_inputs']
            or evidence.get('forced_process_cleanup') or evidence.get('forced_container_cleanup')
            or evidence.get('interruption_signals') or not evidence.get('actual_snapshot_verified')
            or not evidence.get('snapshot_removed')):
        raise ValueError('requires a successfully verified run')
    runner = read(root / 'artifacts/diagnostic-runner.json')
    cleanup = read(root / 'artifacts/runtime-cleanup.json')
    if (runner['mode'] != mode or runner['test_exit_code'] != 0
            or runner['collection_exit_code'] != 0 or runner['forced_test_cleanup']
            or runner['interruption_signals'] or not cleanup['runtime_removed']
            or cleanup['remaining_processes'] or cleanup['processes_recorded'] != 6):
        raise ValueError('requires normal six-process shutdown and runtime cleanup')
    if evidence['platform'] == 'native':
        process_cleanup = evidence['process_cleanup']
        if (not process_cleanup['metadata_present'] or process_cleanup['remaining_processes']
                or process_cleanup['errors']):
            raise ValueError('native process cleanup did not verify')
    elif not evidence['container_cleanup']['container_absent']:
        raise ValueError('owned container cleanup did not verify')
    for filename, key in [('source-sha256.json', 'source_manifest_sha256'), ('run.log', 'log_sha256')]:
        if hashlib.sha256((root / filename).read_bytes()).hexdigest() != evidence[key]:
            raise ValueError('source manifest or completed log identity changed')
    runtime = root / 'artifacts/runtime'
    experiment = read(runtime / 'windows.json')
    if experiment.get('schema_version') != 2 or experiment.get('mode') != mode or experiment['plan']['preflight']:
        raise ValueError('wrong mode, legacy schema, or preflight cannot form a formal pair')
    if ('profile_policy' in runner or 'profile_policy' in experiment['plan']) and runner.get('profile_policy') != experiment['plan'].get('profile_policy'):
        raise ValueError('runner profile policy does not match experiment plan')
    binary = root / 'artifacts/memoryprofile.test'
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    if digest != read(root / 'artifacts/binary-sha256.json')['sha256']:
        raise ValueError('binary no longer matches its recorded identity')
    manifest = read(root / 'artifacts/runtime-files-sha256.json')
    for relative, expected in manifest.items():
        path = runtime / relative
        if not path.resolve().is_relative_to(runtime.resolve()):
            raise ValueError('archive manifest escapes runtime evidence')
        if path.stat().st_size != expected['bytes'] or hashlib.sha256(path.read_bytes()).hexdigest() != expected['sha256']:
            raise ValueError('archived raw evidence was changed')
    spec = importlib.util.spec_from_file_location('memory_summary', Path(__file__).with_name('summarize-memory-windows.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.summarize(runtime)
    summary = read(runtime / 'window-summary.json')
    roles = {'agent', 'daemon', 'sync-client', 'sync-server', 'port-client', 'port-server'}
    if set(summary['roles']) != roles or any(len(value['idle']) != 6 or len(value['loaded']) != 5 for value in summary['roles'].values()):
        raise ValueError('requires all six process series and all eleven boundaries')
    return evidence, experiment, summary, digest, len(manifest)


def compare(control, load, output):
    c_e, c_x, c_s, c_sha, c_files = condition(control, 'idle-control')
    l_e, l_x, l_s, l_sha, l_files = condition(load, 'load')
    if c_e['platform'] != l_e['platform'] or c_sha != l_sha:
        raise ValueError('pair must share platform and exact ordinary binary')
    c_source, l_source = read(control / 'source-sha256.json'), read(load / 'source-sha256.json')
    if c_source != l_source:
        raise ValueError('pair has different frozen source inputs')
    c_plan = {k: v for k, v in c_x['plan'].items() if k != 'mode'}
    l_plan = {k: v for k, v in l_x['plan'].items() if k != 'mode'}
    if c_plan != l_plan:
        raise ValueError('pair has different warmup, duration or sampling plans')
    if c_x['warmup_file_sha256'] != l_x['warmup_file_sha256']:
        raise ValueError('pair warmed different payloads')
    result = {'platform': c_e['platform'], 'binary_sha256': c_sha,
              'source_manifest_sha256': hashlib.sha256((control / 'source-sha256.json').read_bytes()).hexdigest(),
              'source_files': len(c_source), 'control': str(control.resolve()), 'load': str(load.resolve()),
              'raw_files_verified': {'idle-control': c_files, 'load': l_files}, 'plan': c_plan,
              'analysis_tools_sha256': {path.name: hashlib.sha256(path.read_bytes()).hexdigest()
                                        for path in (Path(__file__), Path(__file__).with_name('summarize-memory-windows.py'))},
              'inference': 'one descriptive pair; not a causal, no-leak, or capacity verdict', 'roles': {}}
    for role in c_s['roles']:
        values = {}
        for metric in c_s['roles'][role]['metrics']:
            c = c_s['roles'][role]['metrics'][metric]
            l = l_s['roles'][role]['metrics'][metric]
            values[metric] = {'control_idle': c['idle_values'], 'load_idle': l['idle_values'],
                              'control_delta': c['first_to_last'], 'load_delta': l['first_to_last'],
                              'load_minus_control_delta': l['first_to_last']-c['first_to_last'],
                              'control_late_slope_per_minute': c['ols_per_minute_last_three_idle'],
                              'load_late_slope_per_minute': l['ols_per_minute_last_three_idle'],
                              'load_minus_control_late_slope_per_minute': l['ols_per_minute_last_three_idle']-c['ols_per_minute_last_three_idle']}
        result['roles'][role] = values
    output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({'pair': str(output), 'platform': result['platform'], 'binary_sha256': c_sha}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('control', type=Path)
    parser.add_argument('load', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    compare(args.control, args.load, args.output)
