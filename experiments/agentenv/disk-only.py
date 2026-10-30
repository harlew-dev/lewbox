#!/usr/bin/env python3
"""Rootfs-only feasibility SPIKE, not a supported AgentENV lifecycle API.

Uses guest fsfreeze, private Firecracker pause/resume (no RAM capture), private
ublk sealing RPC, and direct repository publication in PublishedImage format.
Tests full-root cold restore, synthetic secret exclusion, upload failure/retry,
and separately demonstrates native shutdown's remaining RAM-capture problem.
"""
import hashlib
import json
import mmap
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time
import urllib.error
import urllib.parse

import lewbox as lab
from smoke import api, guest
from disklib import fc, prepare_capture
from s3lib import BUCKET, objects, request

HERE = Path(__file__).resolve().parent
ROOT = Path(os.environ['LAB_ROOT'])
RUN = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
NODE = ROOT / 'd'
CAPTURE = ROOT / f'disk-only-{RUN}'
PREFIX = f'disk-only-{RUN}/'
OUTPUT = HERE / 'results' / f'disk-only-{RUN}.json'
CANARY = b'synthetic-memory-only-credential-DO-NOT-PERSIST'
REPORT = {'release': 'v0.2.4', 'prototype_uses_internal_interfaces': True,
          'upstream_source_modified': False, 'prefix': PREFIX, 'tests': []}
lab.NODE, lab.RUN, lab.PREFIX = NODE, RUN, PREFIX


def save():
    OUTPUT.write_text(json.dumps(REPORT, indent=2) + '\n')


def record(name, detail, outcome='passed'):
    entry = {'name': name, 'outcome': outcome, 'detail': detail}
    REPORT['tests'].append(entry)
    save()
    print(json.dumps(entry), flush=True)


def put(key, payload):
    request('PUT', f'/{BUCKET}/' + urllib.parse.quote(key, safe='/'), body=payload)
    actual = request('GET', f'/{BUCKET}/' + urllib.parse.quote(key, safe='/'))
    assert hashlib.sha256(actual).digest() == hashlib.sha256(payload).digest(), key


def publish(image):
    layers = []
    for lower in image['lowers']:
        local = lower.get('file')
        if local:
            path = Path(local)
            assert path.stat().st_size < 512 * 1024**2, 'Small-fixture publisher refuses an oversized/sparse full-disk blob'
            with path.open('rb') as stream, mmap.mmap(stream.fileno(), 0, access=mmap.ACCESS_READ) as data:
                assert data.find(CANARY) < 0, 'Synthetic credential found in a disk layer'
            payload = path.read_bytes()
            digest = 'sha256:' + hashlib.sha256(payload).hexdigest()
            put(f'{PREFIX}managed-layers/{digest}', payload)
            size = len(payload)
        else:
            digest, size = lower['digest'], lower['size']
            payload = request('GET', f'/{BUCKET}/' + urllib.parse.quote(f'{PREFIX}managed-layers/{digest}', safe='/'))
            assert len(payload) == size and 'sha256:' + hashlib.sha256(payload).hexdigest() == digest
            assert CANARY not in payload
        layers.append({'Managed': {'digest': digest, 'size': size}})
    # Trusted fixture configuration only; create-time customer env is deliberately excluded.
    description = {'schemaVersion': 1, 'os': 'linux', 'architecture': 'amd64', 'layers': layers,
                   'config': {'Env': ['PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'],
                              'Cmd': ['/usr/local/bin/lewbox-start'], 'WorkingDir': '/'}}
    encoded = json.dumps(description, sort_keys=True, separators=(',', ':')).encode()
    assert CANARY not in encoded
    digest = 'sha256:' + hashlib.sha256(encoded).hexdigest()
    put(f'{PREFIX}images/descriptions/{digest}.json', encoded)
    return digest, layers


def memory_artifacts():
    home = Path(os.environ['AENV_HOME_PATH'])
    return sorted(str(p.relative_to(home)) for p in home.rglob('*')
        if p.is_file() and (p.name == 'vm_state.bin' or p.name.startswith(('mem_image', 'mem_compacted')) or '/mem_overlaybd/' in str(p)))


def cold(image, canary=False):
    body = {'image': image, 'cpuCount': 1, 'memoryMB': 512, 'diskSizeMB': 20480,
            'timeout': 1800, 'autoPause': False, 'secure': True}
    if canary:
        body['envVars'] = {'LEWBOX_MEMORY_CANARY': CANARY.decode()}
    result, seconds = api('POST', '/sandboxes-cold', body, expected=201)
    return result['sandboxID'], seconds


def fingerprint(sid):
    return guest(sid, "sha256sum /workspace/data.bin /etc/lewbox-owned /root/customer-config; "
        "stat -c '%u:%g:%a:%h' /etc/lewbox-owned; readlink /workspace/link")['stdout']


def start_s3(handles):
    log = (ROOT / f'logs/disk-only-{RUN}-s3.log').open('a')
    handles.append(log)
    process = subprocess.Popen(['python3', str(HERE / 'start-s3.py')], stdout=log, stderr=subprocess.STDOUT)
    handles.append(process)
    lab.wait_http('http://127.0.0.1:19000/minio/health/live')
    return process


def experiments(handles, ids):
    store = start_s3(handles)
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
    lab.start_server('p', handles, allow_shrink=True)
    sid, _ = cold('alpine:3.22', canary=True)
    ids.add(sid)
    guest(sid, "apk add --no-cache util-linux >/dev/null; "
        "mkdir -p /workspace /run/lewbox; mount -t tmpfs -o size=1m tmpfs /run/lewbox; "
        "printf '%s' \"$LEWBOX_MEMORY_CANARY\" > /run/lewbox/credential; "
        "test -n \"$LEWBOX_MEMORY_CANARY\"; printf 'owned-data\\n' > /etc/lewbox-owned; "
        "chown 1234:2345 /etc/lewbox-owned; chmod 640 /etc/lewbox-owned; "
        "ln /etc/lewbox-owned /workspace/hardlink; ln -s /etc/lewbox-owned /workspace/link; "
        "printf 'root-config\\n' > /root/customer-config; "
        "dd if=/dev/urandom of=/workspace/data.bin bs=1048576 count=64; "
        "printf '#!/bin/sh\\necho startup-ran > /workspace/startup-marker\\ncat /proc/sys/kernel/random/boot_id > /workspace/startup-boot-id\\nexec sleep 1800\\n' > /usr/local/bin/lewbox-start; "
        "chmod 755 /usr/local/bin/lewbox-start; sync")
    before = fingerprint(sid)
    boot = guest(sid, 'cat /proc/sys/kernel/random/boot_id')['stdout'].strip()
    work, image, _ = prepare_capture(sid, CAPTURE / 'first.commit')
    digest, layers = publish(image)
    assert not memory_artifacts(), memory_artifacts()
    uploaded = objects(PREFIX)
    assert all('/managed-layers/' in x['key'] or '/images/descriptions/' in x['key'] for x in uploaded)
    record('full_root_disk_only_publication', {'image_digest': digest, 'layers': layers,
        'published_bytes': sum(x['bytes'] for x in uploaded), 'memory_artifacts': [],
        'synthetic_canary_excluded_from_disk_layers_and_manifest': True,
        'fixture_paths': ['/etc/lewbox-owned', '/root/customer-config', '/workspace/data.bin'],
        'note': 'Internal seal + repository image publication, not the public snapshot endpoint'})
    api('DELETE', f'/sandboxes/{sid}', expected=204)
    ids.remove(sid)
    lab.stop_server()
    assert not memory_artifacts()
    shutil.rmtree(NODE / 'p')
    shutil.rmtree(CAPTURE)
    CAPTURE.mkdir()
    record('source_runtime_and_local_disk_data_removed', {'publisher_state_removed': True, 'capture_layers_removed': True})
    lab.start_server('r', handles, allow_shrink=True)
    restored, seconds = cold(digest)
    ids.add(restored)
    assert fingerprint(restored) == before
    new_boot = guest(restored, 'cat /proc/sys/kernel/random/boot_id')['stdout'].strip()
    assert new_boot != boot
    guest(restored, "test ! -e /run/lewbox/credential; test -z \"${LEWBOX_MEMORY_CANARY:-}\"; "
        "test $(cat /sys/class/block/vdb/size) = 41943040")
    automatic = guest(restored, 'if test -f /workspace/startup-marker; then echo yes; else echo no; fi')['stdout'].strip() == 'yes'
    record('cold_create_automatically_runs_image_cmd', {'startup_marker_created': automatic,
        'note': 'A disk-only adapter must invoke startup/readiness when the cold-create API does not'},
        outcome='passed' if automatic else 'blocked')
    if not automatic:
        guest(restored, '/usr/local/bin/lewbox-start </dev/null >/dev/null 2>&1 &')
    for attempt in range(20):
        observed = guest(restored, 'cat /workspace/startup-boot-id 2>/dev/null || true')['stdout'].strip()
        if observed == new_boot:
            break
        time.sleep(.1)
    assert observed == new_boot, 'Explicit startup did not execute in the fresh VM'
    record('fresh_state_cold_restore_entire_root', {'cold_create_seconds': seconds, 'boot_id_changed': True,
        'startup_command_executed': True, 'adapter_invoked_startup': not automatic,
        'files_metadata_hardlink_symlink_verified': True,
        'credential_tmpfs_file_and_create_time_env_absent': True, 'disk_bytes': 20 * 1024**3})

    guest(restored, "printf 'uncommitted-write\\n' > /root/after-checkpoint; sync")
    work, image, sealed = prepare_capture(restored, CAPTURE / 'failed-upload.commit')
    store.terminate()
    store.wait(timeout=15)
    try:
        publish(image)
    except OSError:
        pass
    else:
        raise AssertionError('Publication unexpectedly succeeded with S3 stopped')
    assert sealed.exists(), 'Failed publish removed a layer owned by the live VM'
    store = start_s3(handles)
    fc(work, 'PATCH', '/vm', {'state': 'Resumed'})
    guest(restored, 'fsfreeze -u /')
    assert fingerprint(restored) == before
    guest(restored, 'test $(cat /root/after-checkpoint) = uncommitted-write')
    record('prototype_upload_failure_retains_live_root', {'sandbox_not_deleted': True,
        'sealed_layer_retained': True, 'resumed_files_verified': True,
        'note': 'Harness rollback policy, not a native AgentENV capture-failure regression test'})
    # Re-capture the recovered source and publish a new immutable description.
    _, image, _ = prepare_capture(restored, CAPTURE / 'retry.commit')
    retry_digest, _ = publish(image)
    assert retry_digest != digest
    api('DELETE', f'/sandboxes/{restored}', expected=204)
    ids.remove(restored)
    latest, _ = cold(retry_digest)
    ids.add(latest)
    guest(latest, 'test $(cat /root/after-checkpoint) = uncommitted-write')
    assert fingerprint(latest) == before
    api('DELETE', f'/sandboxes/{latest}', expected=204)
    ids.remove(latest)
    previous, _ = cold(digest)
    ids.add(previous)
    guest(previous, 'test ! -e /root/after-checkpoint')
    assert fingerprint(previous) == before
    api('DELETE', f'/sandboxes/{previous}', expected=204)
    ids.remove(previous)
    assert not memory_artifacts()
    record('retry_and_previous_checkpoint_recovery', {'retry_digest': retry_digest,
        'new_write_durable_after_retry': True, 'previous_checkpoint_unchanged': True,
        'no_memory_artifacts_before_native_control': True})

    # Separate native-behaviour control: graceful shutdown is not governed by autoPause=False.
    control, _ = cold('alpine:3.22', canary=True)
    ids.add(control)
    lab.stop_server()
    artifacts = memory_artifacts()
    assert artifacts, 'Expected the stock graceful shutdown to persist paused VM state'
    record('native_shutdown_never_captures_customer_memory', {'auto_pause': False,
        'memory_state_artifacts_created': artifacts,
        'note': 'Public/runtime shutdown policy still needs a supported disk-only path'}, 'blocked')
    # Remove the persisted control record through the running node before final cleanup.
    lab.start_server('r', handles, allow_shrink=True)
    api('DELETE', f'/sandboxes/{control}', expected=204)
    ids.remove(control)
    REPORT['execution_passed'] = True
    REPORT['full_lewbox_gate_passed'] = False
    save()


def main():
    assert not NODE.exists(), f'Refusing to overwrite {NODE}'
    for port in [18080, 19000, 19103]:
        with socket.socket() as sock:
            assert sock.connect_ex(('127.0.0.1', port)) != 0, f'Port {port} in use'
    NODE.mkdir()
    CAPTURE.mkdir()
    handles, ids = [], set()
    attempted = False
    try:
        attempted = True
        lab.run('bash', str(HERE / 'setup-host.sh'))
        experiments(handles, ids)
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
        if attempted:
            try:
                lab.run('bash', str(HERE / 'cleanup-host.sh'))
            except Exception as error:
                errors.append(str(error))
        for handle in reversed(handles):
            if isinstance(handle, subprocess.Popen):
                try:
                    handle.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    handle.terminate()
                    handle.wait(timeout=15)
            else:
                handle.close()
        REPORT['cleanup'] = {'passed': not errors, 'errors': errors}
        save()
        if errors:
            raise RuntimeError(f'Cleanup incomplete: {errors}')
    print(f'Results: {OUTPUT}', flush=True)


if __name__ == '__main__':
    main()
