#!/usr/bin/env python3
"""Build the synthetic Dockerfile twice and launch the resulting template."""
import json
import os
from pathlib import Path
import subprocess
import time

from smoke import api, cli, guest

HERE = Path(__file__).parent
report = {"release": "v0.2.4", "builds": []}
output = HERE / "results/build.json"
run_id = str(int(time.time()))
for label in ["initial", "cached"]:
    template_name = f"lewbox-lab-build-{run_id}-{label}"
    logfile = HERE / f"results/build-{label}.log"
    started = time.monotonic()
    with logfile.open("w") as stream:
        proc = subprocess.Popen(["aenv", "build", str(HERE / "fixture"), "--name", template_name,
            "--cpu", "1", "--memory", "512", "--timeout", "300", "--progress", "plain",
            "--build-arg", "LAB_VALUE=checked"], stdout=stream, stderr=subprocess.STDOUT)
        try:
            code = proc.wait(timeout=900)
        except subprocess.TimeoutExpired:
            proc.terminate()
            code = proc.wait(timeout=15)
    entry = {"label": label, "template_name": template_name, "elapsed_seconds": round(time.monotonic() - started, 4), "exit_code": code}
    report["builds"].append(entry)
    output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(entry), flush=True)
    if code:
        raise SystemExit(code)
sandbox = cli("start", template_name, "--timeout", "600", "-d")["stdout"].strip()
try:
    result = guest(sandbox, "cat /opt/lewbox/marker.txt /opt/lewbox/build-arg.txt /tmp/lewbox-startup; "
        "stat -c '%u:%g:%a' /opt/lewbox/marker.txt")
    assert "checked\n" in result["stdout"] and "startup-ran" in result["stdout"] and "1234:2345:640" in result["stdout"]
    report["template_verification"] = result
    report["passed"] = True
    output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(result), flush=True)
finally:
    api("DELETE", f"/sandboxes/{sandbox}", expected=204)
