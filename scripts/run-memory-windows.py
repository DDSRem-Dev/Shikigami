#!/usr/bin/env python3
"""Own one short local diagnostic runtime and forward cancellation before archiving."""

import json
import argparse
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time


def run(binary, evidence, preflight, idle_control=False, defer_profiles=False):
    evidence = evidence.resolve()
    parent = Path(tempfile.mkdtemp(prefix='shiki-mw.', dir='/tmp')).resolve()
    mode = 'idle-control' if idle_control else 'load'
    profile_policy = 'after-close' if defer_profiles else 'each-boundary'
    result = {'runtime_parent': str(parent), 'mode': mode, 'interruption_signals': [], 'test_exit_code': None,
              'forced_test_cleanup': False, 'collection_exit_code': None, 'profile_policy': profile_policy}
    (evidence / 'diagnostic-runner.json').write_text(json.dumps(result, indent=2) + '\n')
    args = [str(binary.resolve()), '-test.run=^TestRepeatedWorkerMemoryWindows$', '-test.v',
            '-test.timeout=8m', '-memoryprofile-output', str(parent / 'r'), '-memoryprofile-mode', mode]
    if preflight:
        args.append('-memoryprofile-preflight')
    child = None
    stopping_at = None
    previous = {}

    def stop(signum, _frame):
        nonlocal stopping_at
        result['interruption_signals'].append(signum)
        if stopping_at is None:
            stopping_at = time.monotonic()
        if child is not None and child.poll() is None:
            child.send_signal(signum)  # Only the test controller; its cleanup owns hosts/workers.

    try:
        for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            previous[sig] = signal.signal(sig, stop)
        child = subprocess.Popen(args, start_new_session=True,
                                 env={**os.environ, 'SHIKI_MEMORY_PROFILE_POLICY': profile_policy})
        result['test_pid'] = child.pid
        deadline = time.monotonic() + 8 * 60
        while child.poll() is None:
            if time.monotonic() >= deadline and stopping_at is None:
                result['timed_out'] = True
                stop(signal.SIGTERM, None)
            if stopping_at is not None and time.monotonic() - stopping_at >= 25:
                result['forced_test_cleanup'] = True
                os.killpg(child.pid, signal.SIGKILL)
                break
            time.sleep(0.1)
        result['test_exit_code'] = child.wait(timeout=10)
    finally:
        if child is not None and child.poll() is None:
            child.send_signal(signal.SIGTERM)
            try:
                child.wait(timeout=25)
            except subprocess.TimeoutExpired:
                result['forced_test_cleanup'] = True
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=10)
        if child is not None:
            result['test_exit_code'] = child.poll()
        collected = subprocess.run([sys.executable, 'scripts/collect-memory-windows.py', str(parent), str(evidence)], timeout=30)
        result['collection_exit_code'] = collected.returncode
        (evidence / 'diagnostic-runner.json').write_text(json.dumps(result, indent=2) + '\n')
        for sig, handler in previous.items():
            signal.signal(sig, handler)
    if result['interruption_signals']:
        return 128 + result['interruption_signals'][0]
    return 0 if result['test_exit_code'] == 0 and result['collection_exit_code'] == 0 and not result['forced_test_cleanup'] else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary', type=Path)
    parser.add_argument('evidence', type=Path)
    parser.add_argument('--preflight', action='store_true')
    parser.add_argument('--idle-control', action='store_true')
    parser.add_argument('--defer-profiles', action='store_true', help='export host profiles only after close; workers retain JSON samples but no profiles')
    args = parser.parse_args()
    sys.exit(run(args.binary, args.evidence, args.preflight, args.idle_control, args.defer_profiles))
