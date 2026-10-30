#!/usr/bin/env python3
"""Real KVM experiments for Lewbox's 75 GB / three 20 GB sandbox model.

Uses a disposable sparse 75,000,000,000-byte ext4 loop filesystem and Silo.
Never invokes pause. The quota and disk-only cold-start requirements may be
BLOCKED even when all assertions about the observed release behaviour pass.
The default scenario leaves shrinking disabled and uses sparse 64 GiB fallback
disks; pressure.py separately tests three full 20 GiB disks with shrinking enabled.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

from smoke import api, cli, guest
from s3lib import BUCKET, credentials, objects, request

HERE = Path(__file__).resolve().parent
ROOT = Path(os.environ['LAB_ROOT'])
NODE = ROOT / 'n'
IMAGE = ROOT / 'lewbox-node.img'
RUN = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
PREFIX = f'lewbox-{RUN}/'
OUTPUT = HERE / 'results' / f'lewbox-{RUN}.json'
REPORT = {'release': 'v0.2.4', 'node_backing_bytes': 75_000_000_000,
          'requested_sandbox_bytes': 20_000_000_000,
          'requested_api_disk_mib': 20480,
          'requested_api_disk_bytes': 20 * 1024**3,
          'payload_bytes_per_sandbox': 128 * 1024**2,
          'real_r2_tested': False, 'prefix': PREFIX, 'tests': [], 'storage': [],
          'scope': '75 GB raw filesystem for node state/runtime; OS/binaries and S3 fixture outside it',
          'pause_api_used': False}


def save():
    OUTPUT.write_text(json.dumps(REPORT, indent=2) + '\n')


def record(name, detail, outcome='passed'):
    entry = {'name': name, 'outcome': outcome, 'detail': detail}
    REPORT['tests'].append(entry)
    save()
    print(json.dumps(entry), flush=True)


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout.strip()


def allocated(path):
    """Count physical blocks once per inode, including hard-linked layer files."""
    seen = set()
    total = 0
    for base, dirs, files in os.walk(path):
        for name in dirs + files:
            try:
                stat = (Path(base) / name).lstat()
            except FileNotFoundError:
                continue
            inode = stat.st_dev, stat.st_ino
            if inode not in seen:
                seen.add(inode)
                total += stat.st_blocks * 512
    return total


def measure(stage):
    os.sync()
    stat = os.statvfs(NODE)
    data = {'stage': stage, 'filesystem_used_bytes': (stat.f_blocks - stat.f_bfree) * stat.f_frsize,
            'filesystem_available_bytes': stat.f_bavail * stat.f_frsize,
            'all_node_files_allocated_bytes': allocated(NODE),
            'image_cache_allocated_bytes': allocated(NODE / 'p/image-cache'),
            'snapshot_cache_allocated_bytes': allocated(NODE / 'p/snapshot-local-cache'),
            'workspaces_allocated_bytes': allocated(NODE / 'p/firecracker-work'),
            'backing_file_host_allocated_bytes': IMAGE.stat().st_blocks * 512}
    REPORT['storage'].append(data)
    save()
    print(json.dumps(data), flush=True)
    return data


def wait_http(url):
    deadline = time.monotonic() + 40
    while time.monotonic() < deadline:
        try:
            urllib.request.urlopen(url, timeout=1).close()
            return
        except OSError:
            time.sleep(.25)
    raise RuntimeError(f'Fixture not healthy: {url}')


def configure(role, allow_shrink=False, oss_endpoint='http://127.0.0.1:19000'):
    home = NODE / role
    config = (ROOT / 'config.toml').read_text().replace(str(ROOT / 'state'), str(home))
    config = config.replace('repository_backend = "posix_fs"', 'repository_backend = "oss"')
    if allow_shrink:
        config = config.replace('allow_shrink = false', 'allow_shrink = true', 1)
    creds = credentials()
    config += f'''\n[backend.oss]
endpoint = "{oss_endpoint}"
bucket = "{BUCKET}"
region = "us-east-1"
prefix = "{PREFIX}"
addressing_style = "path"
access_key_id = "{creds['access']}"
access_key_secret = "{creds['secret']}"
cache_max_size_gb = 1
'''
    target = ROOT / f'lewbox-{role}.toml'
    target.touch(mode=0o600)
    target.write_text(config)
    os.environ.update(AENV_HOME_PATH=str(home), AENV_RUNTIME_PATH=str(NODE / f'{role}r'),
                      AENV_CONFIG_PATH=str(target))


def start_server(role, handles, allow_shrink=False, oss_endpoint='http://127.0.0.1:19000'):
    configure(role, allow_shrink=allow_shrink, oss_endpoint=oss_endpoint)
    log = (ROOT / f'logs/lewbox-{RUN}-{role}.log').open('a')
    handles.append(log)
    process = subprocess.Popen(['bash', str(HERE / 'start.sh')], stdout=log, stderr=subprocess.STDOUT)
    handles.append(process)
    wait_http('http://127.0.0.1:18080/health')
    run('python3', str(HERE / 'auth.py'))


def stop_server():
    run('bash', str(HERE / 'stop.sh'))


def remote_record(snapshot_id):
    return json.loads(request('GET', f'/{BUCKET}/{PREFIX}catalog/records/{snapshot_id}.json'))


def verify_published(snapshot_id):
    """Verify the persisted catalog plus every referenced managed layer's hash."""
    committed = remote_record(snapshot_id)['committed']
    layers = [x['Managed'] for x in committed['rootfs_layers'] if 'Managed' in x]
    layers += committed['memory_layers']
    sizes = {x['key']: x['bytes'] for x in objects(PREFIX)}
    for layer in layers:
        key = f"{PREFIX}managed-layers/{layer['digest']}"
        assert sizes[key] == layer['size'], key
        payload = request('GET', f'/{BUCKET}/' + urllib.parse.quote(key, safe='/'))
        assert 'sha256:' + hashlib.sha256(payload).hexdigest() == layer['digest'], key
    assert f'{PREFIX}artifacts/{snapshot_id}/vm_state.bin' in sizes
    return committed


def capture(sandbox, label):
    result, seconds = api('POST', f'/sandboxes/{sandbox}/snapshots',
                          {'name': f'lewbox-{RUN}-{label}'}, expected=201)
    snapshot_id = result['snapshotID']
    committed = verify_published(snapshot_id)
    REPORT.setdefault('snapshots', []).append(snapshot_id)
    save()
    return snapshot_id, committed, seconds


def launch(snapshot_id):
    result, seconds = api('POST', '/sandboxes',
        {'templateID': snapshot_id, 'timeout': 1800, 'autoPause': False, 'secure': True}, expected=201)
    return result['sandboxID'], seconds


def remove_runtime(sid):
    _, seconds = api('DELETE', f'/sandboxes/{sid}', expected=204)
    api('GET', f'/sandboxes/{sid}', expected=404)
    return seconds


def fingerprint(sid):
    return guest(sid, "cat /proc/sys/kernel/random/boot_id; "
        "sha256sum /workspace/payload.bin; stat -c '%u:%g:%a' /workspace/owned; "
        "readlink /workspace/link; cat /workspace/owned; "
        "pid=$(cat /workspace/sleeper.pid); test -d /proc/$pid; cat /proc/$pid/comm")['stdout']


def experiments(handles):
    start_server('p', handles)
    baseline = measure('server_ready')
    rejections = []
    body = {'image': 'alpine:3.22', 'cpuCount': 1, 'memoryMB': 512, 'diskSizeMB': 20480,
            'timeout': 1800, 'autoPause': False, 'secure': True}
    for slot in range(3):
        error, seconds = api('POST', '/sandboxes-cold', body, expected=400)
        assert 'smaller than base virtual size 68719476736' in error['message'], error
        rejections.append({'slot': slot, 'http_status': 400, 'message': error['message'], 'seconds': seconds})
    record('three_20g_guest_disks', {'attempts': rejections,
        'note': '20480 MiB = 20 GiB, greater than 20 decimal GB; both fall below the 64 GiB floor'}, 'blocked')

    sources = []
    for slot in range(3):
        result, seconds = api('POST', '/sandboxes-cold', dict(body, diskSizeMB=65536,
            envVars={'LEWBOX_LAB_CANARY': 'synthetic-lewbox-stop-canary'}), expected=201)
        sid = result['sandboxID']
        guest(sid, f"mkdir -p /workspace; printf 'slot-{slot}\\n' > /workspace/owned; "
            "chown 1234:2345 /workspace/owned; chmod 640 /workspace/owned; ln -s owned /workspace/link; "
            "dd if=/dev/urandom of=/workspace/payload.bin bs=1048576 count=128; sync; "
            "sleep 1800 </dev/null >/dev/null 2>&1 & echo $! > /workspace/sleeper.pid")
        sectors = guest(sid, 'cat /sys/class/block/vdb/size')['stdout'].strip()
        assert int(sectors) * 512 == 64 * 1024**3
        sources.append({'slot': slot, 'sandbox_id': sid, 'fingerprint': fingerprint(sid),
                        'guest_disk_bytes': int(sectors) * 512, 'start_api_seconds': seconds})
    REPORT['sources'] = sources
    live = measure('three_running_with_128m_each')
    assert live['all_node_files_allocated_bytes'] < sum(x['guest_disk_bytes'] for x in sources)
    record('three_sparse_fallback_vms', {'actual_allocated_node_bytes': live['all_node_files_allocated_bytes'],
        'guest_logical_bytes': sum(x['guest_disk_bytes'] for x in sources),
        'note': 'Fallback 64 GiB disks, not a successful three-by-20 GB capacity qualification'})

    for source in sources:
        sid = source['sandbox_id']
        snapshot_id, committed, seconds = capture(sid, f"stop-{source['slot']}")
        source.update(snapshot_id=snapshot_id, committed=committed, snapshot_api_seconds=seconds)
        source['remote_verified_before_delete'] = True
        measure(f"slot_{source['slot']}_published_before_runtime_delete")
        source['delete_api_seconds'] = remove_runtime(sid)
        assert remote_record(snapshot_id)['committed'] == committed
    records, _ = api('GET', '/v2/sandboxes')
    assert not records
    after_delete = measure('all_three_published_and_runtime_deleted')
    assert after_delete['workspaces_allocated_bytes'] < 1024**2, 'Deleted writable workspaces were retained'
    assert after_delete['all_node_files_allocated_bytes'] < live['all_node_files_allocated_bytes'], 'No node space reclaimed'
    record('stop_publish_verify_then_delete', {'sources': [
        {k: s[k] for k in ['sandbox_id', 'snapshot_id', 'remote_verified_before_delete', 'delete_api_seconds']}
        for s in sources], 'remaining_runtime_records': len(records)})
    memory = sum(x['size'] for s in sources for x in s['committed']['memory_layers'])
    assert memory > 0
    record('disk_only_stop', {'published_memory_layer_bytes': memory,
        'note': 'No pause calls; the explicit snapshot publication still includes RAM'}, 'blocked')
    record('immediate_local_space_reclamation', {
        'live_node_allocated_bytes': live['all_node_files_allocated_bytes'],
        'after_delete_node_allocated_bytes': after_delete['all_node_files_allocated_bytes'],
        'net_bytes_reclaimed': live['all_node_files_allocated_bytes'] - after_delete['all_node_files_allocated_bytes'],
        'remaining_image_cache_bytes': after_delete['image_cache_allocated_bytes'],
        'remaining_snapshot_cache_bytes': after_delete['snapshot_cache_allocated_bytes'],
        'note': 'Counts actual physical blocks, not 20 GB reservations; cached data is reported separately'})

    # Cold-start contract: the only available persistent restore primitive is a full snapshot.
    child, seconds = launch(sources[0]['snapshot_id'])
    first = fingerprint(child)
    assert first == sources[0]['fingerprint']
    record('start_after_runtime_delete', {'files_and_metadata_preserved': True,
        'start_api_seconds': seconds, 'new_runtime_id': child})
    record('start_is_a_cold_boot', {'same_boot_id_and_live_process': True,
        'note': 'Snapshot restore kept RAM/processes; not a disk-only cold boot'}, 'blocked')

    guest(child, "printf 'restart-write\\n' > /workspace/restart-marker; sync")
    restarted_snapshot, _, _ = capture(child, 'restart')
    remove_runtime(child)
    restarted, seconds = launch(restarted_snapshot)
    assert fingerprint(restarted) == first
    assert guest(restarted, 'cat /workspace/restart-marker')['stdout'].strip() == 'restart-write'
    remove_runtime(restarted)
    record('snapshot_based_restart_retains_files', {'restart_start_api_seconds': seconds,
        'cold_reboot': False, 'note': 'Emulated publish/delete/restore cycle, not a native reboot API'})

    # Full node-state eviction is safe only because all runtimes are gone and publication was verified.
    stop_server()
    before_eviction = measure('publisher_stopped_before_offline_eviction')
    shutil.rmtree(NODE / 'p')
    if (NODE / 'pr').exists():
        shutil.rmtree(NODE / 'pr')
    after_eviction = measure('publisher_state_evicted_offline')
    assert after_eviction['all_node_files_allocated_bytes'] == 0
    record('offline_whole_node_eviction', {
        'reclaimed_allocated_bytes': before_eviction['all_node_files_allocated_bytes'],
        'remaining_node_file_allocated_bytes': after_eviction['all_node_files_allocated_bytes'],
        'note': 'Disposable stopped node state removed by this harness; not an AgentENV per-sandbox eviction API'})

    start_server('r', handles)
    assert not (NODE / 'p').exists()
    restored = []
    for source in sources:
        sid, seconds = launch(source['snapshot_id'])
        assert fingerprint(sid) == source['fingerprint']
        restored.append({'slot': source['slot'], 'start_api_seconds': seconds})
        remove_runtime(sid)
    record('restore_all_three_after_complete_local_eviction', {'restores': restored,
        'publisher_state_exists': (NODE / 'p').exists(), 'same_physical_host': True})

    before_persistent_delete = objects(PREFIX)
    for snapshot_id in [sources[0]['snapshot_id'], restarted_snapshot]:
        api('DELETE', f'/snapshots/{snapshot_id}', expected=204)
        api('GET', f'/snapshots/{snapshot_id}', expected=404)
    after_persistent_delete = objects(PREFIX)
    removed = {x['key'] for x in before_persistent_delete} - {x['key'] for x in after_persistent_delete}
    assert any('/catalog/records/' in x for x in removed)
    assert any('/artifacts/' in x for x in removed)
    # An unrelated stopped sandbox must still restore from the same shared lower layer.
    sibling, _ = launch(sources[1]['snapshot_id'])
    assert fingerprint(sibling) == sources[1]['fingerprint']
    remove_runtime(sibling)
    remaining = [x for x in after_persistent_delete if '/managed-layers/' in x['key']]
    referenced = set()
    for obj in after_persistent_delete:
        if '/catalog/records/' in obj['key']:
            document = json.loads(request('GET', f'/{BUCKET}/' + obj['key']))
            committed = document.get('committed') or {}
            referenced.update(x['Managed']['digest'] for x in committed.get('rootfs_layers', []) if 'Managed' in x)
            referenced.update(x['digest'] for x in committed.get('memory_layers', []))
    unreferenced = [x for x in remaining if x['key'].rsplit('/', 1)[1] not in referenced]
    assert referenced.issubset({x['key'].rsplit('/', 1)[1] for x in remaining}), 'Deletion removed a surviving layer'
    record('persistent_delete_preserves_other_sandboxes', {'deleted_snapshot_ids': [sources[0]['snapshot_id'], restarted_snapshot],
        'removed_object_count': len(removed), 'remaining_managed_layer_count': len(remaining),
        'unreferenced_managed_layers_after_delete': unreferenced,
        'unreferenced_managed_layer_bytes': sum(x['bytes'] for x in unreferenced),
        'sibling_restore_verified': True, 'note': 'Shared/unreferenced object GC is distinct from catalog deletion'})
    REPORT['storage_baseline_used_bytes'] = baseline['filesystem_used_bytes']
    REPORT['execution_passed'] = True
    save()


def main(experiment=None):
    experiment = experiments if experiment is None else experiment
    handles = []
    loop_device = None
    mounted = False
    setup_attempted = False
    image_created = False
    node_created = False
    try:
        for port in [18080, 19000, 19103]:
            with socket.socket() as sock:
                assert sock.connect_ex(('127.0.0.1', port)) != 0, f'Port {port} is already in use; stop the lab first'
        assert not IMAGE.exists() and not NODE.exists(), 'Refusing to overwrite existing fixture paths'
        save()
        setup_attempted = True
        run('bash', str(HERE / 'setup-host.sh'))
        NODE.mkdir()
        node_created = True
        with IMAGE.open('xb') as stream:
            stream.truncate(REPORT['node_backing_bytes'])
        image_created = True
        loop_device = run('sudo', '-n', 'losetup', '--find', '--show', str(IMAGE))
        run('sudo', '-n', 'mkfs.ext4', '-q', '-F', '-m', '0', '-T', 'largefile4',
            '-E', 'lazy_itable_init=0,lazy_journal_init=0', loop_device)
        run('sudo', '-n', 'mount', '-o', 'nodev,nosuid', loop_device, str(NODE))
        mounted = True
        run('sudo', '-n', 'chown', f'{os.getuid()}:{os.getgid()}', str(NODE))
        # No lost+found data is needed in this synthetic filesystem.
        run('sudo', '-n', 'rmdir', str(NODE / 'lost+found'))
        measure('empty_75gb_filesystem')
        log = (ROOT / f'logs/lewbox-{RUN}-s3.log').open('a')
        handles.append(log)
        process = subprocess.Popen(['python3', str(HERE / 'start-s3.py')], stdout=log, stderr=subprocess.STDOUT)
        handles.append(process)
        wait_http('http://127.0.0.1:19000/minio/health/live')
        for attempt in range(60):
            try:
                request('PUT', f'/{BUCKET}')
                break
            except urllib.error.HTTPError as error:
                body = error.read()
                if error.code == 409 and b'BucketAlreadyOwnedByYou' in body:
                    break
                if error.code != 503 or attempt == 59:
                    raise
                time.sleep(.25)
        experiment(handles)
    except Exception as error:
        REPORT.update(execution_passed=False, error=str(error))
        # Keep bounded converter diagnostics before the disposable filesystem is
        # removed. Configs, environment and object bodies are never copied.
        diagnostics = []
        for role in ('p', 'r'):
            log = NODE / role / 'overlaybd/overlaybd.log'
            if log.is_file():
                with log.open('rb') as stream:
                    stream.seek(max(0, log.stat().st_size - 128 * 1024))
                    diagnostics.append(stream.read().decode(errors='replace'))
        if diagnostics:
            text = '\n'.join(diagnostics)
            for value in credentials().values():
                if isinstance(value, str):
                    text = text.replace(value, '[REDACTED]')
            for role in ('p', 'r'):
                key = NODE / role / 'secrets/api-key'
                if key.is_file():
                    text = text.replace(key.read_text().strip(), '[REDACTED]')
            target = OUTPUT.with_suffix('.diagnostics.log')
            target.write_text(text)
            REPORT['diagnostics'] = target.name
        save()
        raise
    finally:
        cleanup_errors = []
        if setup_attempted:
            try:
                run('bash', str(HERE / 'cleanup-host.sh'))
            except Exception as error:
                cleanup_errors.append(str(error))
        for handle in reversed(handles):
            if isinstance(handle, subprocess.Popen):
                try:
                    handle.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    handle.terminate()
                    handle.wait(timeout=10)
            else:
                handle.close()
        if mounted:
            try:
                run('sudo', '-n', 'umount', str(NODE))
                mounted = False
            except Exception as error:
                cleanup_errors.append(str(error))
        if loop_device and not mounted:
            try:
                run('sudo', '-n', 'losetup', '-d', loop_device)
            except Exception as error:
                cleanup_errors.append(str(error))
        if not mounted and not cleanup_errors:
            if image_created:
                IMAGE.unlink()
            if node_created:
                NODE.rmdir()
        REPORT['cleanup'] = {'passed': not cleanup_errors, 'errors': cleanup_errors,
                             'filesystem_image_removed': not IMAGE.exists(), 'mountpoint_removed': not NODE.exists()}
        save()
        if cleanup_errors:
            raise RuntimeError(f'Lab cleanup incomplete: {cleanup_errors}')
    print(f'Results: {OUTPUT}', flush=True)


if __name__ == '__main__':
    main()
