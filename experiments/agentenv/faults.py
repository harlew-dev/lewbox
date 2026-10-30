#!/usr/bin/env python3
"""Native snapshot publication failure with S3 unavailable, not a mock uploader.

Creates a verified durable baseline, mutates the root disk, stops only Silo,
requests an AgentENV snapshot, then checks live-layer ownership and baseline
restoration after the store returns. All payloads/credentials are synthetic.
"""
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request

import lewbox as lab
from smoke import api, guest
from s3lib import objects
from faultproxy import StoreProxy

RUN = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
UPLOAD_FAILURE = os.environ.get('LEWBOX_FAULT_KIND') == 'upload'
lab.RUN = RUN
lab.PREFIX = f'faults-{RUN}/'
lab.OUTPUT = lab.HERE / 'results' / f'faults-{RUN}.json'
lab.REPORT = {'release': 'v0.2.4', 'node_backing_bytes': 75_000_000_000,
              'allow_shrink': True, 'tests': [], 'storage': [], 'prefix': lab.PREFIX,
              'scope': 'Native checkpoint API with managed-layer PUT failures' if UPLOAD_FAILURE else
                       'Native checkpoint API while local S3 fixture is unavailable', 'real_r2_tested': False}


def response(method, route, body=None):
    key = (Path(os.environ['AENV_HOME_PATH']) / 'secrets/api-key').read_text().strip()
    request = urllib.request.Request('http://127.0.0.1:18080' + route, method=method,
        data=json.dumps(body).encode() if body is not None else None,
        headers={'X-API-Key': key, 'Content-Type': 'application/json'})
    started = time.monotonic()
    try:
        with urllib.request.urlopen(request, timeout=180) as result:
            status, data = result.status, result.read()
    except urllib.error.HTTPError as error:
        status, data = error.code, error.read()
    parsed = json.loads(data) if data else {}
    return {'http_status': status, 'state': parsed.get('state'), 'message': parsed.get('message'),
            'elapsed_seconds': round(time.monotonic() - started, 4)}


def missing_live_layers():
    missing = []
    home = Path(os.environ['AENV_HOME_PATH'])
    for config in home.glob('firecracker-work/*/overlaybd/image.json'):
        for lower in json.loads(config.read_text()).get('lowers', []):
            if lower.get('file'):
                path = Path(lower['file'])
                if not path.is_absolute():
                    path = config.parent / path
                if not path.exists():
                    missing.append(str(path))
    return missing


def experiments(handles):
    temporary = lab.NODE / 'tmp'
    temporary.mkdir()
    os.environ['TMPDIR'] = str(temporary)
    proxy = StoreProxy() if UPLOAD_FAILURE else None
    if proxy:
        proxy.start()
        handles.append(proxy)
    lab.start_server('p', handles, allow_shrink=True,
                     oss_endpoint='http://127.0.0.1:19002' if proxy else 'http://127.0.0.1:19000')
    sandbox, _ = api('POST', '/sandboxes-cold', {'image': 'alpine:3.22', 'cpuCount': 1,
        'memoryMB': 512, 'diskSizeMB': 20480, 'timeout': 1800, 'autoPause': False, 'secure': True}, expected=201)
    sid = sandbox['sandboxID']
    guest(sid, 'mkdir -p /workspace; dd if=/dev/urandom of=/workspace/first.bin bs=1048576 count=32; sync')
    checksum = guest(sid, 'sha256sum /workspace/first.bin')['stdout']
    saved, _, _ = lab.capture(sid, 'durable')
    guest(sid, "dd if=/dev/urandom of=/workspace/second.bin bs=1048576 count=16; sync")
    second = guest(sid, 'sha256sum /workspace/second.bin')['stdout']
    assert not missing_live_layers()
    before_objects = {x['key'] for x in objects(lab.PREFIX)}
    before = {key for key in before_objects if '/catalog/records/' in key}
    silo = next(handle for handle in handles if isinstance(handle, subprocess.Popen)
                and str(handle.args[-1]).endswith('start-s3.py'))
    if proxy:
        proxy.reject_uploads.set()
    else:
        silo.terminate()
        silo.wait(timeout=15)
    print(json.dumps({'progress': 'native snapshot with layer PUT failure' if proxy else
                     'native snapshot with S3 unavailable'}), flush=True)
    result = response('POST', f'/sandboxes/{sid}/snapshots', {'name': f'faults-{RUN}-offline'})
    assert result['http_status'] == 500, result
    state = response('GET', f'/sandboxes/{sid}')
    missing = missing_live_layers()
    if proxy:
        proxy.reject_uploads.clear()
        assert proxy.failures > 0, 'Failure did not reach a managed-layer PUT'
        assert proxy.successful_puts_before_failure == 1, 'Partial publication must include one completed layer PUT'
        lab.REPORT['injected_layer_put_failures'] = proxy.failures
        lab.REPORT['successful_layer_puts_before_failure'] = proxy.successful_puts_before_failure
        lab.save()
    else:
        logfile = (lab.ROOT / f'logs/faults-{RUN}-s3-restarted.log').open('a')
        handles.append(logfile)
        restarted = subprocess.Popen(['python3', str(lab.HERE / 'start-s3.py')], stdout=logfile, stderr=subprocess.STDOUT)
        handles.append(restarted)
    lab.wait_http('http://127.0.0.1:19000/minio/health/ready')
    for attempt in range(60):
        try:
            after_objects = objects(lab.PREFIX)
            after = {x['key'] for x in after_objects if '/catalog/records/' in x['key']}
            break
        except urllib.error.HTTPError as error:
            if error.code != 503 or attempt == 59:
                raise
            time.sleep(.25)
    # Lazy disk reads may require S3; qualify data recovery after the store returns.
    intact = False
    if state['state'] == 'running':
        try:
            intact = (guest(sid, 'sha256sum /workspace/first.bin')['stdout'] == checksum and
                      guest(sid, 'sha256sum /workspace/second.bin')['stdout'] == second)
        except Exception:
            pass
    lab.record('native_s3_failure_keeps_live_data_and_layers', {'capture_response': result,
        'runtime_after_error': state, 'both_files_readable_after_s3_returns': intact, 'missing_live_layers': missing},
        outcome='passed' if intact and not missing else 'blocked')
    assert before == after
    lab.record('failed_upload_does_not_commit_new_checkpoint', {'catalog_records_unchanged': True,
        'new_managed_objects_before_retry': [x for x in after_objects if x['key'] not in before_objects
                                             and '/managed-layers/' in x['key']]})
    if intact and not missing:
        retry, _, _ = lab.capture(sid, 'retry')
        lab.remove_runtime(sid)
        recovered, _ = lab.launch(retry)
        assert guest(recovered, 'sha256sum /workspace/first.bin')['stdout'] == checksum
        assert guest(recovered, 'sha256sum /workspace/second.bin')['stdout'] == second
        lab.remove_runtime(recovered)
        lab.record('native_retry_restores_latest_files', {'snapshot_id': retry, 'both_files_verified': True})
    elif state['http_status'] == 200:
        deleted = response('DELETE', f'/sandboxes/{sid}')
        assert deleted['http_status'] == 204, deleted
    previous, _ = lab.launch(saved)
    assert guest(previous, 'sha256sum /workspace/first.bin')['stdout'] == checksum
    guest(previous, 'test ! -e /workspace/second.bin')
    lab.remove_runtime(previous)
    lab.record('known_durable_checkpoint_survives_s3_outage', {'snapshot_id': saved, 'checksum_verified': True})
    lab.REPORT['execution_passed'] = True
    lab.save()


if __name__ == '__main__':
    lab.main(experiments)
