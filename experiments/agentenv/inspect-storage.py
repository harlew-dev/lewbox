#!/usr/bin/env python3
"""Measure POSIX layers and search only for the synthetic test canary."""
import json
import mmap
import os
from pathlib import Path
import sys

here = Path(__file__).parent
runs = [json.loads(p.read_text()) for p in sorted((here / 'results').glob('smoke-*.json'))]
run = next(r for r in reversed(runs) if r.get('passed'))
if len(sys.argv) > 1:
    run = json.loads(Path(sys.argv[1]).read_text())
root = Path(os.environ['AENV_HOME_PATH']) / 'snapshot-store/repository'
report = {}
for label in ['baseline', 'delta']:
    sid = run[f'{label}_snapshot_id']
    record = json.loads((root / f'catalog/records/{sid}.json').read_text())['committed']
    disk = [x['Managed'] for x in record['rootfs_layers'] if 'Managed' in x]
    memory = record['memory_layers']
    found = []
    for layer in memory:
        path = root / 'managed-layers' / f"{layer['digest'].replace(':', '_')}.overlaybd.commit"
        with path.open('rb') as stream, mmap.mmap(stream.fileno(), 0, access=mmap.ACCESS_READ) as data:
            found.append(data.find(b'synthetic-lab-canary-no-real-credentials') >= 0)
    report[label] = dict(snapshot_id=sid, disk_layers=disk, memory_layers=memory,
                         disk_bytes=sum(x['size'] for x in disk), memory_bytes=sum(x['size'] for x in memory),
                         synthetic_canary_found_in_memory_layers=found)
old = {x['digest'] for x in report['baseline']['disk_layers']}
report['incremental_disk_bytes'] = sum(x['size'] for x in report['delta']['disk_layers'] if x['digest'] not in old)
report['reused_disk_bytes'] = sum(x['size'] for x in report['delta']['disk_layers'] if x['digest'] in old)
output = Path(sys.argv[2]) if len(sys.argv) > 2 else here / 'results/local-storage.json'
output.write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({k: v for k, v in report.items() if not isinstance(v, dict)}))
