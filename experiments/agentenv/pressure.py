#!/usr/bin/env python3
"""Three real 20 GiB guests on a 75 GB filesystem, plus native ENOSPC recovery.

Writes incompressible data until each guest filesystem is full. After deleting
those test VMs, injects node filesystem pressure around a small durable baseline.
All server temporary files are inside the bounded node filesystem. No real R2.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import urllib.error
import urllib.request

import lewbox as lab
from smoke import api, guest
from s3lib import objects

HERE, ROOT, NODE = lab.HERE, lab.ROOT, lab.NODE
RUN = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
lab.RUN = RUN
lab.PREFIX = f'pressure-{RUN}/'
lab.OUTPUT = HERE / 'results' / f'pressure-{RUN}.json'
lab.REPORT = {'release': 'v0.2.4', 'node_backing_bytes': 75_000_000_000,
              'disk_bytes_per_guest': 20 * 1024**3, 'allow_shrink': True,
              'real_r2_tested': False, 'prefix': lab.PREFIX, 'tests': [], 'storage': [],
              'scope': 'Node state/runtime/temp files on 75 GB raw ext4; OS, binaries and S3 fixture outside'}


def response(method, route, body=None):
    key = (Path(os.environ['AENV_HOME_PATH']) / 'secrets/api-key').read_text().strip()
    request = urllib.request.Request('http://127.0.0.1:18080' + route, method=method,
        data=json.dumps(body).encode() if body is not None else None,
        headers={'X-API-Key': key, 'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(request, timeout=180) as result:
            status, data = result.status, result.read()
    except urllib.error.HTTPError as error:
        status, data = error.code, error.read()
    parsed = json.loads(data) if data else {}
    # Never return or log successful responses containing access tokens.
    return {'http_status': status, 'state': parsed.get('state'), 'message': parsed.get('message')}


def cold():
    data, seconds = api('POST', '/sandboxes-cold', {'image': 'alpine:3.22', 'cpuCount': 1,
        'memoryMB': 512, 'diskSizeMB': 20480, 'timeout': 3600, 'autoPause': False, 'secure': True}, expected=201)
    return data['sandboxID'], seconds


def experiments(handles):
    assert shutil.disk_usage(ROOT).free > 70 * 1024**3, 'Need at least 70 GiB host headroom for real data writes'
    temporary = NODE / 'tmp'
    temporary.mkdir()
    os.environ['TMPDIR'] = str(temporary)
    lab.start_server('p', handles, allow_shrink=True)
    baseline = lab.measure('three_20g_server_ready')
    guests = []
    for slot in range(3):
        sid, seconds = cold()
        guest(sid, f"mkdir -p /workspace; printf 'slot-{slot}\\n' > /workspace/sentinel; "
            "test $(cat /sys/class/block/vdb/size) = 41943040; sync")
        guests.append({'slot': slot, 'sandbox_id': sid, 'create_seconds': seconds})
    lab.REPORT['guests'] = guests
    lab.record('three_concurrent_20g_devices', {'guests': guests})
    processes = []
    command = ("set +e; dd if=/dev/urandom of=/workspace/full.bin bs=1048576 count=24576; "
        "code=$?; sync; echo DD_EXIT=$code; stat -c FILE_BYTES=%s /workspace/full.bin; df -k /; "
        "test $code = 1")
    for item in guests:
        path = ROOT / f"logs/pressure-{RUN}-fill-{item['slot']}.log"
        log = path.open('w')
        handles.append(log)
        process = subprocess.Popen(['aenv', 'exec', item['sandbox_id'], '--', 'sh', '-c', command],
            stdout=log, stderr=subprocess.STDOUT)
        processes.append((process, path))
        handles.append(process)
    deadline = time.monotonic() + 1200
    last_update = 0
    while any(p.poll() is None for p, _ in processes):
        assert time.monotonic() < deadline, 'Guest fill exceeded 20-minute deadline'
        assert shutil.disk_usage(ROOT).free > 12 * 1024**3, 'Host safety reserve reached; aborting isolated pressure test'
        if time.monotonic() - last_update > 15:
            stat = os.statvfs(NODE)
            print(json.dumps({'progress': 'writing three guest disks',
                'node_available_bytes': stat.f_bavail * stat.f_frsize}), flush=True)
            last_update = time.monotonic()
        time.sleep(1)
    for item, (process, path) in zip(guests, processes):
        log = path.read_text()
        assert process.returncode == 0 and 'No space left on device' in log and 'DD_EXIT=1' in log, log[-1200:]
        size = int(next(x.split('=', 1)[1] for x in log.splitlines() if x.startswith('FILE_BYTES=')))
        assert 18 * 1024**3 < size < 20 * 1024**3, size
        item['written_file_bytes'] = size
        assert guest(item['sandbox_id'], 'cat /workspace/sentinel')['stdout'].strip() == f"slot-{item['slot']}"
    full = lab.measure('all_three_guest_filesystems_enospc')
    assert full['filesystem_available_bytes'] > 5 * 1024**3, 'ENOSPC must be per guest, not the host/node filesystem'
    lab.record('guest_enospc_isolated_from_node', {'guests': guests,
        'node_available_bytes': full['filesystem_available_bytes'], 'all_sentinels_verified': True})
    first = guests[0]
    lab.remove_runtime(first['sandbox_id'])
    after_one = lab.measure('first_full_guest_deleted')
    assert full['all_node_files_allocated_bytes'] - after_one['all_node_files_allocated_bytes'] > 18 * 1024**3
    for item in guests[1:]:
        assert guest(item['sandbox_id'], 'cat /workspace/sentinel')['stdout'].strip() == f"slot-{item['slot']}"
    lab.record('delete_full_guest_reclaims_disk_and_preserves_neighbors', {
        'reclaimed_bytes': full['all_node_files_allocated_bytes'] - after_one['all_node_files_allocated_bytes'],
        'neighbors_readable': True})
    for item in guests[1:]:
        lab.remove_runtime(item['sandbox_id'])
    after_all = lab.measure('all_full_guests_deleted')
    assert after_all['all_node_files_allocated_bytes'] < 512 * 1024**2
    # Return holes in the loop backing file to the host before the next phase.
    trimmed = subprocess.run(['sudo', '-n', 'fstrim', str(NODE)], capture_output=True, text=True)
    lab.record('full_guest_cleanup', {'node_allocated_bytes': after_all['all_node_files_allocated_bytes'],
        'loop_backing_trim_supported': trimmed.returncode == 0,
        'note': 'OS/binaries excluded; full checkpoint staging is a separate capacity requirement'})

    sid, _ = cold()
    guest(sid, "mkdir -p /workspace; dd if=/dev/urandom of=/workspace/baseline.bin bs=1048576 count=32; sync")
    checksum = guest(sid, 'sha256sum /workspace/baseline.bin')['stdout']
    saved, _, _ = lab.capture(sid, 'durable-baseline')
    guest(sid, "dd if=/dev/urandom of=/workspace/pressure.bin bs=1048576 count=4096; sync")
    reserve = NODE / 'reserve-for-enospc'
    available = os.statvfs(NODE).f_bavail * os.statvfs(NODE).f_frsize
    with reserve.open('xb') as file:
        os.posix_fallocate(file.fileno(), 0, available - 128 * 1024**2)
    pressure = lab.measure('node_reserve_leaves_128m_for_capture')
    before = {x['key'] for x in objects(lab.PREFIX) if '/catalog/records/' in x['key']}
    print(json.dumps({'progress': 'native checkpoint with node ENOSPC injection'}), flush=True)
    result = response('POST', f'/sandboxes/{sid}/snapshots', {'name': f'pressure-{RUN}-must-not-commit'})
    reserve.unlink()
    assert result['http_status'] == 500, result
    after = {x['key'] for x in objects(lab.PREFIX) if '/catalog/records/' in x['key']}
    assert after == before, 'Failed snapshot must not publish a new committed catalog record'
    state = response('GET', f'/sandboxes/{sid}')
    intact = False
    if state['state'] == 'running':
        try:
            intact = guest(sid, 'sha256sum /workspace/baseline.bin')['stdout'] == checksum
        except Exception:
            pass
    lab.record('native_capture_enospc_recovery', {'capture_response': result, 'runtime_after_error': state,
        'running_source_baseline_readable': intact, 'no_new_catalog_record': True,
        'known_durable_snapshot_id': saved}, outcome='passed' if intact else 'blocked')
    if state['http_status'] == 200:
        deleted = response('DELETE', f'/sandboxes/{sid}')
        assert deleted['http_status'] == 204, deleted
    restored, _ = lab.launch(saved)
    assert guest(restored, 'sha256sum /workspace/baseline.bin')['stdout'] == checksum
    guest(restored, 'test ! -e /workspace/pressure.bin')
    lab.remove_runtime(restored)
    lab.record('previous_durable_checkpoint_survives_node_enospc', {'checksum_verified': True,
        'uncommitted_write_absent': True, 'snapshot_id': saved})
    lab.measure('pressure_test_end')
    lab.REPORT['execution_passed'] = True
    lab.save()


if __name__ == '__main__':
    lab.main(experiments)
