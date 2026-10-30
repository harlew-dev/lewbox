#!/usr/bin/env python3
"""Qualify v0.2.4's existing allow_shrink setting on a disposable cold VM.

No upstream source edits. Checks the device AND ext4 filesystem, plus snapshot
reuse of the smaller shape. Does not claim a host physical-storage quota.
"""
import json
import os
from pathlib import Path
import socket
import subprocess
import time

from smoke import api, guest

HERE = Path(__file__).resolve().parent
ROOT = Path(os.environ['LAB_ROOT'])
RUN = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
OUTPUT = HERE / 'results' / f'shrink-{RUN}.json'
REPORT = {'release': 'v0.2.4', 'allow_shrink': True, 'upstream_source_modified': False, 'tests': []}


def save():
    OUTPUT.write_text(json.dumps(REPORT, indent=2) + '\n')


def record(name, detail):
    REPORT['tests'].append({'name': name, 'passed': True, 'detail': detail})
    save()
    print(json.dumps(REPORT['tests'][-1]), flush=True)


def resources(sid):
    output = guest(sid, 'cat /sys/class/block/vdb/size; df -k /')['stdout']
    lines = output.splitlines()
    assert int(lines[0]) * 512 == 20 * 1024**3, output
    blocks = int(lines[-1].split()[1])
    assert 18 * 1024**2 < blocks < 20 * 1024**2, output
    return {'device_bytes': int(lines[0]) * 512, 'filesystem_total_kib': blocks, 'guest_output': output}


def main():
    for port in [18080, 19103]:
        with socket.socket() as sock:
            assert sock.connect_ex(('127.0.0.1', port)) != 0, f'Port {port} in use'
    home = ROOT / 's'
    assert not home.exists(), f'Refusing to reuse existing state: {home}'
    config = (ROOT / 'config.toml').read_text().replace(str(ROOT / 'state'), str(home))
    assert config.count('allow_shrink = false') == 1
    config = config.replace('allow_shrink = false', 'allow_shrink = true', 1)
    target = ROOT / f'config-shrink-{RUN}.toml'
    target.write_text(config)
    os.environ.update(AENV_CONFIG_PATH=str(target), AENV_HOME_PATH=str(home), AENV_RUNTIME_PATH=str(ROOT / 'sr'))
    process = None
    setup_attempted = False
    log = (ROOT / f'logs/shrink-{RUN}.log').open('a')
    ids = []
    try:
        setup_attempted = True
        subprocess.run(['bash', str(HERE / 'setup-host.sh')], check=True, stdout=log, stderr=subprocess.STDOUT)
        process = subprocess.Popen(['bash', str(HERE / 'start.sh')], stdout=log, stderr=subprocess.STDOUT)
        subprocess.run(['python3', str(HERE / 'auth.py')], check=True)
        sandbox, seconds = api('POST', '/sandboxes-cold', {'image': 'alpine:3.22', 'cpuCount': 1,
            'memoryMB': 512, 'diskSizeMB': 20480, 'timeout': 1800, 'autoPause': False, 'secure': True}, expected=201)
        sid = sandbox['sandboxID']
        ids.append(sid)
        record('cold_create_20g_with_allow_shrink', {'sandbox_id': sid, 'api_seconds': seconds, **resources(sid)})
        guest(sid, "mkdir -p /workspace; printf 'shrunk-fixture\\n' > /workspace/owned; "
            "chmod 640 /workspace/owned; chown 1234:2345 /workspace/owned; ln -s owned /workspace/link; "
            "dd if=/dev/urandom of=/workspace/data.bin bs=1048576 count=32; sync")
        command = "sha256sum /workspace/data.bin; stat -c '%u:%g:%a' /workspace/owned; readlink /workspace/link; cat /workspace/owned"
        fingerprint = guest(sid, command)['stdout']
        record('write_and_metadata_after_shrink', {'fingerprint': fingerprint})
        captured, seconds = api('POST', f'/sandboxes/{sid}/snapshots', {'name': f'lewbox-20g-{RUN}'}, expected=201)
        snapshot_id = captured['snapshotID']
        record('capture_20g_snapshot', {'snapshot_id': snapshot_id, 'api_seconds': seconds})
        api('DELETE', f'/sandboxes/{sid}', expected=204)
        ids.remove(sid)
        restored, seconds = api('POST', '/sandboxes', {'templateID': snapshot_id, 'timeout': 600,
            'autoPause': False, 'secure': True}, expected=201)
        child = restored['sandboxID']
        ids.append(child)
        assert guest(child, command)['stdout'] == fingerprint
        record('warm_restore_retains_20g_and_files', {'sandbox_id': child, 'api_seconds': seconds, **resources(child)})
        REPORT['execution_passed'] = True
        save()
    except Exception as error:
        REPORT.update(execution_passed=False, error=str(error))
        save()
        raise
    finally:
        errors = []
        for sid in ids:
            try:
                api('DELETE', f'/sandboxes/{sid}', expected=204)
            except Exception as error:
                errors.append(str(error))
        if setup_attempted:
            result = subprocess.run(['bash', str(HERE / 'cleanup-host.sh')], stdout=log, stderr=subprocess.STDOUT)
            if result.returncode:
                errors.append(f'Host cleanup exited {result.returncode}')
        if process is not None:
            process.wait(timeout=20)
        log.close()
        REPORT['cleanup'] = {'passed': not errors, 'errors': errors}
        save()
        if errors:
            raise RuntimeError(f'Cleanup incomplete: {errors}')
    print(f'Results: {OUTPUT}', flush=True)


if __name__ == '__main__':
    main()
