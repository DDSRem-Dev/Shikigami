#!/usr/bin/env python3
"""Archive this diagnostic's local runtime after joining its owned processes."""

import json
import hashlib
import os
from pathlib import Path
import shutil
import stat
import sys
import time


def collect(parent, evidence):
    parent, evidence = parent.resolve(), evidence.resolve()
    if parent.parent != Path('/tmp').resolve() or not parent.name.startswith('shiki-mw.'):
        raise ValueError('expected the private runner-created temporary directory')
    source, destination = parent / 'r', evidence / 'runtime'
    records = []
    for path in sorted(source.glob('*.ready.json')):
        ready = json.loads(path.read_text())
        records.append({'role': path.name.removesuffix('.ready.json'), 'pid': ready['PID'],
                        'created_ms': ready['created_ms'], 'executable': ready['executable']})
    (evidence / 'processes.json').write_text(json.dumps({'processes': records}, indent=2) + '\n')
    remaining = []
    for _ in range(30):
        remaining = []
        for record in records:
            if not isinstance(record['pid'], int) or record['pid'] <= 1:
                raise ValueError('invalid recorded process identity')
            try:
                os.kill(record['pid'], 0)
                remaining.append(record)
            except ProcessLookupError:
                pass
            except PermissionError:
                remaining.append(record)
        if not remaining:
            break
        time.sleep(0.1)
    omitted = []

    def ignore(directory, names):
        skipped = []
        for name in names:
            path = Path(directory) / name
            mode = path.lstat().st_mode
            if not (stat.S_ISREG(mode) or stat.S_ISDIR(mode)):
                skipped.append(name)
                omitted.append(str(path.relative_to(source)))
        return skipped

    if source.exists():
        shutil.copytree(source, destination, ignore=ignore)
        destination.chmod(0o700)
    manifest = {}
    for directory, subdirs, files in os.walk(source, followlinks=False):
        subdirs[:] = [name for name in subdirs if not (Path(directory) / name).is_symlink()]
        for name in files:
            path = Path(directory) / name
            if not stat.S_ISREG(path.lstat().st_mode):
                continue
            relative = path.relative_to(source)
            digest = hashlib.sha256(path.read_bytes()).hexdigest()
            copied = destination / relative
            if hashlib.sha256(copied.read_bytes()).hexdigest() != digest:
                raise RuntimeError(f'archived file differs from local evidence: {relative}')
            manifest[str(relative)] = {'bytes': path.stat().st_size, 'sha256': digest}
    (evidence / 'runtime-files-sha256.json').write_text(json.dumps(manifest, indent=2) + '\n')
    if not remaining:
        shutil.rmtree(parent)
    cleanup = {'runtime_parent': str(parent), 'runtime_removed': not parent.exists(),
               'remaining_processes': remaining, 'omitted_special_entries': omitted,
               'processes_recorded': len(records)}
    (evidence / 'runtime-cleanup.json').write_text(json.dumps(cleanup, indent=2) + '\n')
    if remaining:
        raise RuntimeError('owned processes remain; preserve runtime for outer identity-checked cleanup')


if __name__ == '__main__':
    if len(sys.argv) != 3:
        raise SystemExit('usage: collect-memory-windows.py RUNTIME_PARENT EVIDENCE')
    collect(Path(sys.argv[1]), Path(sys.argv[2]))
