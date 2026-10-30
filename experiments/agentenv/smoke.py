#!/usr/bin/env python3
"""Exercise the isolated v0.2.4 lab. Requires lab.env.sh and a running server.

Creates synthetic data only. Leaves one paused sandbox and named snapshots for
restart/inspection; deletes temporary forks. Credentials stay outside the repo.
"""
import hashlib
import json
import os
from pathlib import Path
import pty
import select
import struct
import subprocess
import termios
import fcntl
import time
import urllib.error
import urllib.request

ROOT = Path(os.environ["LAB_ROOT"])
OUT = Path(__file__).parent / "results" / f"smoke-{time.strftime('%Y%m%d-%H%M%S', time.gmtime())}.json"
OUT.parent.mkdir(exist_ok=True)
REPORT = {"release": "v0.2.4", "tests": []}


def save():
    OUT.write_text(json.dumps(REPORT, indent=2) + "\n")


def api(method, path, body=None, expected=200):
    key = (Path(os.environ["AENV_HOME_PATH"]) / "secrets/api-key").read_text().strip()
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(
        "http://127.0.0.1:18080" + path, data=data, method=method,
        headers={"X-API-Key": key, "Content-Type": "application/json"},
    )
    started = time.monotonic()
    try:
        with urllib.request.urlopen(request, timeout=180) as response:
            status, raw = response.status, response.read()
    except urllib.error.HTTPError as error:
        status, raw = error.code, error.read()
    elapsed = round(time.monotonic() - started, 4)
    try:
        result = json.loads(raw) if raw else None
    except json.JSONDecodeError:
        result = raw.decode()
    if status != expected:
        # Successful responses include access tokens; never record their bodies.
        detail = result.get("message", "") if isinstance(result, dict) else str(result)
        raise RuntimeError(f"{method} {path}: HTTP {status} (expected {expected}): {detail[:1200]}")
    return result, elapsed


def cli(*args, expected=0):
    result = subprocess.run(["aenv", *args], capture_output=True, text=True, timeout=180)
    if result.returncode != expected:
        raise RuntimeError(f"aenv {args[0]} exited {result.returncode}: {result.stderr[:1200]}")
    return {"stdout": result.stdout, "stderr": result.stderr, "exit_code": result.returncode}


def guest(sandbox, command, expected=0):
    return cli("exec", sandbox, "--", "sh", "-c", "set -e; " + command, expected=expected)


def record(name, function):
    started = time.monotonic()
    try:
        detail = function()
        entry = {"name": name, "passed": True, "detail": detail}
    except Exception as error:
        entry = {"name": name, "passed": False, "error": str(error)}
    entry["elapsed_seconds"] = round(time.monotonic() - started, 4)
    REPORT["tests"].append(entry)
    save()
    print(json.dumps(entry), flush=True)
    if not entry["passed"]:
        raise RuntimeError(name)
    return entry["detail"]


def cold(cpu=1, memory=512, disk=65536):
    body = {"image": "alpine:3.22", "cpuCount": cpu, "memoryMB": memory,
            "diskSizeMB": disk, "timeout": 1800, "autoPause": True, "secure": True,
            "envVars": {"LEWBOX_LAB_CANARY": "synthetic-lab-canary-no-real-credentials"}}
    result, elapsed = api("POST", "/sandboxes-cold", body, expected=201)
    return {"sandbox_id": result["sandboxID"], "api_seconds": elapsed}


def pty_check(sandbox):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 100, 0, 0))
    proc = subprocess.Popen(["aenv", "connect", sandbox], stdin=slave, stdout=slave, stderr=slave)
    os.close(slave)
    captured = bytearray()
    deadline = time.monotonic() + 20
    sent = False
    try:
        while time.monotonic() < deadline:
            ready, _, _ = select.select([master], [], [], 0.2)
            if ready:
                try:
                    chunk = os.read(master, 65536)
                except OSError:
                    break
                if not chunk:
                    break
                captured.extend(chunk)
            if not sent and time.monotonic() > deadline - 18:
                os.write(master, b"printf '__LEWBOX_PTY__\\n'; stty size; exit\n")
                sent = True
            if proc.poll() is not None:
                break
        output = captured.decode(errors="replace")
        assert "40 100" in output and "__LEWBOX_PTY__" in output, output
        return {"rows": 40, "columns": 100, "shell_command_observed": True}
    finally:
        if proc.poll() is None:
            proc.terminate()
        proc.wait(timeout=5)
        os.close(master)


def fixture(sandbox):
    return guest(sandbox, "mkdir -p /workspace; printf 'base-data\\n' > /workspace/owned; "
                 "chown 1234:2345 /workspace/owned; chmod 640 /workspace/owned; "
                 "ln -s owned /workspace/link; "
                 "sleep 1200 </dev/null >/dev/null 2>&1 & echo $! > /workspace/sleeper.pid; "
                 "cat /proc/sys/kernel/random/boot_id; "
                 "stat -c '%u:%g:%a' /workspace/owned; readlink /workspace/link; "
                 "sha256sum /workspace/owned; printenv LEWBOX_LAB_CANARY")


def identity(sandbox):
    return guest(sandbox, "cat /proc/sys/kernel/random/boot_id; "
                 "pid=$(cat /workspace/sleeper.pid); "
                 "test -d /proc/$pid; cat /proc/$pid/comm; "
                 "stat -c '%u:%g:%a' /workspace/owned; readlink /workspace/link; "
                 "sha256sum /workspace/owned")["stdout"]


def transfer(sandbox):
    source = ROOT / "run/upload.bin"
    source.write_bytes(os.urandom(1024 * 1024))
    dest = ROOT / "run/download.bin"
    cli("upload", sandbox, str(source), "/workspace/upload.bin")
    cli("download", "--force", sandbox, "/workspace/upload.bin", str(dest))
    assert source.read_bytes() == dest.read_bytes()
    return {"bytes": source.stat().st_size, "sha256": hashlib.sha256(source.read_bytes()).hexdigest()}


def snapshot(sandbox, suffix):
    result, elapsed = api("POST", f"/sandboxes/{sandbox}/snapshots", {"name": f"lewbox-lab-{OUT.stem}-{suffix}"}, expected=201)
    return {"snapshot_id": result["snapshotID"], "api_seconds": elapsed}


def fork(snapshot_id):
    result, elapsed = api("POST", "/sandboxes", {"templateID": snapshot_id, "secure": True, "timeout": 1800}, expected=201)
    return {"sandbox_id": result["sandboxID"], "api_seconds": elapsed}


def main():
    # This intentionally validates the release's actual disk floor.
    body, elapsed = api("POST", "/sandboxes-cold", {"image": "alpine:3.22", "cpuCount": 1,
        "memoryMB": 512, "diskSizeMB": 1024, "timeout": 60, "secure": True}, expected=400)
    REPORT["disk_1g_rejection"] = {"response": body, "api_seconds": elapsed}
    primary = record("cached_oci_cold_start", cold)["sandbox_id"]
    REPORT["primary_sandbox_id"] = primary
    save()
    record("guest_resources_and_egress", lambda: guest(primary, "test $(nproc) = 1; "
           "test $(cat /sys/class/block/vdb/size) = 134217728; "
           "awk '/MemTotal/ {exit !($2 > 450000 && $2 < 524288)}' /proc/meminfo; "
           "nproc; head -n 3 /proc/meminfo; df -k /; cat /sys/class/block/vdb/size; "
           "wget -qO /dev/null https://example.com; echo egress-ok"))
    larger = record("resource_override_2cpu_768m_65g", lambda: cold(2, 768, 66560))["sandbox_id"]
    try:
        record("larger_guest_resources", lambda: guest(larger, "test $(nproc) = 2; "
               "test $(cat /sys/class/block/vdb/size) = 136314880; "
               "awk '/MemTotal/ {exit !($2 > 700000 && $2 < 786432)}' /proc/meminfo; "
               "nproc; head -n 1 /proc/meminfo; cat /sys/class/block/vdb/size; df -k /"))
    finally:
        api("DELETE", f"/sandboxes/{larger}", expected=204)
    streams = record("stdout_stderr_exit42", lambda: guest(primary, "echo stdout-marker; echo stderr-marker >&2; exit 42", expected=42))
    assert "stdout-marker" in streams["stdout"] and "stderr-marker" in streams["stderr"]
    record("pty_attach_geometry", lambda: pty_check(primary))
    record("file_metadata_and_background_process", lambda: fixture(primary))
    record("upload_download_roundtrip", lambda: transfer(primary))
    before = identity(primary)
    record("pause", lambda: {"api_seconds": api("POST", f"/sandboxes/{primary}/pause", expected=204)[1]})
    record("resume", lambda: {"api_seconds": api("POST", f"/sandboxes/{primary}/connect", {"timeout": 1800}, expected=201)[1]})
    def retained():
        after = identity(primary)
        assert before == after, (before, after)
        return {"identity": after}
    record("resume_retains_kernel_process_files", retained)
    baseline = record("snapshot_baseline", lambda: snapshot(primary, "baseline"))["snapshot_id"]
    REPORT["baseline_snapshot_id"] = baseline
    child = record("fork_from_hot_snapshot", lambda: fork(baseline))["sandbox_id"]
    try:
        def fork_metadata():
            after = identity(child)
            assert before == after, (before, after)
            guest(child, "printf 'child-write\\n' > /workspace/owned")
            assert guest(primary, "cat /workspace/owned")["stdout"].strip() == "base-data"
            return {"identity": after, "child_write_isolated": True}
        record("fork_preserves_metadata_process_and_cow", fork_metadata)
    finally:
        api("DELETE", f"/sandboxes/{child}", expected=204)
    record("write_8m_random_delta", lambda: guest(primary, "dd if=/dev/urandom of=/workspace/random.bin bs=1048576 count=8; sync; sha256sum /workspace/random.bin"))
    delta = record("snapshot_after_8m_delta", lambda: snapshot(primary, "delta"))["snapshot_id"]
    REPORT["delta_snapshot_id"] = delta
    record("leave_paused_for_restart", lambda: {"api_seconds": api("POST", f"/sandboxes/{primary}/pause", expected=204)[1]})
    REPORT["passed"] = True
    save()
    print(f"Saved {OUT}", flush=True)


if __name__ == "__main__":
    main()
