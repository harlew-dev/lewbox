#!/usr/bin/env python3
"""After stop.sh/start.sh, verify the last successful smoke run survives."""
import json
from pathlib import Path
from smoke import api, guest, identity, fork

here = Path(__file__).parent
runs = [json.loads(p.read_text()) for p in sorted((here / "results").glob("smoke-*.json"))]
run = next(r for r in reversed(runs) if r.get("passed"))
sid = run["primary_sandbox_id"]
detail, _ = api("GET", f"/sandboxes/{sid}")
assert detail['state'] == 'paused'
_, elapsed = api("POST", f"/sandboxes/{sid}/connect", {"timeout": 1800}, expected=201)
observed = identity(sid)
before = next(t['detail']['identity'] for t in run['tests'] if t['name'] == 'resume_retains_kernel_process_files')
assert before == observed
child = fork(run['delta_snapshot_id'])
try:
    copied = guest(child['sandbox_id'], "sha256sum /workspace/random.bin")['stdout']
    expected = next(t['detail']['stdout'] for t in run['tests'] if t['name']=='write_8m_random_delta')
    assert copied == expected
finally:
    api("DELETE", f"/sandboxes/{child['sandbox_id']}", expected=204)
api("POST", f"/sandboxes/{sid}/pause", expected=204)
report = {"passed": True, "sandbox_id": sid, "resume_api_seconds": elapsed,
          "kernel_process_metadata_retained": True, "snapshot_fork_after_restart_verified": True,
          "fork_api_seconds": child['api_seconds']}
(here / "results/restart.json").write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps(report))
