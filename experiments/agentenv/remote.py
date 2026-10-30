#!/usr/bin/env python3
"""Run 'publish', then restart with blank restore state and run 'restore'."""
import json
from pathlib import Path
import sys

from smoke import api, cold, guest, fixture, identity, snapshot, fork
from s3lib import objects

here = Path(__file__).parent
output = here / "results/remote.json"
phase = sys.argv[1]
if phase == "publish":
    report = {"fixture": "Silo RELEASE.2026-09-16T00-00-00Z, loopback, path addressing", "real_r2_tested": False}
    primary = cold()['sandbox_id']
    report['sandbox_id'] = primary
    fixture(primary)
    before = identity(primary)
    base = snapshot(primary, 'remote-baseline')
    first = objects()
    guest(primary, "dd if=/dev/urandom of=/workspace/remote-random.bin bs=1048576 count=8; sync")
    checksum = guest(primary, "sha256sum /workspace/remote-random.bin")['stdout']
    delta = snapshot(primary, 'remote-delta')
    second = objects()
    api("POST", f"/sandboxes/{primary}/pause", expected=204)
    after_pause = objects()
    assert after_pause == second
    report.update(baseline=base, delta=delta, expected_identity=before, expected_checksum=checksum,
                  baseline_objects=first, delta_objects=second, local_pause_uploaded_new_objects=False)
    first_keys = {x['key'] for x in first}
    new = [x for x in second if x['key'] not in first_keys]
    report['new_objects_after_delta'] = new
    report['baseline_object_bytes'] = sum(x['bytes'] for x in first)
    report['new_object_bytes_after_delta'] = sum(x['bytes'] for x in new)
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({k: report[k] for k in ['baseline', 'delta', 'baseline_object_bytes', 'new_object_bytes_after_delta', 'local_pause_uploaded_new_objects']}))
elif phase == 'restore':
    report = json.loads(output.read_text())
    child = fork(report['delta']['snapshot_id'])
    try:
        observed = identity(child['sandbox_id'])
        assert observed == report['expected_identity']
        checksum = guest(child['sandbox_id'], "sha256sum /workspace/remote-random.bin")['stdout']
        assert checksum == report['expected_checksum']
        report['fresh_state_restore'] = {'passed': True, 'api_seconds': child['api_seconds'],
            'metadata_background_process_and_checksum_retained': True}
    finally:
        api('DELETE', f"/sandboxes/{child['sandbox_id']}", expected=204)
    report['passed'] = True
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report['fresh_state_restore']))
else:
    raise SystemExit('Expected publish or restore')
