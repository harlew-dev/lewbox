#!/usr/bin/env python3
"""Actual legacy agentd in a 20 GiB KVM guest, using its scripted test adapter.

No account credentials/model calls. Tests daemon CLI, events, approvals, queue
recovery and upload replay across a private disk-only full-root cold restore.
"""
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import time
import urllib.parse

import lewbox as lab
from disklib import prepare_capture
from s3lib import BUCKET, request
from smoke import api, cli, guest

RUN = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
IMAGE = 'sha256:37b23f77a4460f799efa5ebc6d8dd7c060f3fb95ae8deefd406ce09fcf41299b'
lab.RUN, lab.PREFIX = RUN, 'workload-20261030-212321/'
lab.OUTPUT = lab.HERE / 'results' / f'agentd-{RUN}.json'
lab.REPORT = {'release': 'v0.2.4', 'prefix': lab.PREFIX, 'tests': [], 'storage': [],
              'node_backing_bytes': 75_000_000_000, 'disk_bytes': 20 * 1024**3,
              'scope': 'Unchanged legacy agentd binary; repository fake ACP adapter; local upload receiver',
              'real_agent_adapters_tested': False, 'authenticated_agent_calls_tested': False,
              'real_gateway_tested': False, 'real_r2_tested': False}


def obj(key):
    return f'/{BUCKET}/' + urllib.parse.quote(lab.PREFIX + key, safe='/')


def cold(image):
    data, seconds = api('POST', '/sandboxes-cold', {'image': image, 'cpuCount': 2,
        'memoryMB': 4096, 'diskSizeMB': 20480, 'timeout': 3600,
        'autoPause': False, 'secure': True}, expected=201)
    return data['sandboxID'], seconds


def write(sid, path, data):
    guest(sid, f"printf '%s' {shlex.quote(data)} > {shlex.quote(path)}")


def call(sid, body):
    write(sid, '/tmp/agentd-request.json', json.dumps(body))
    result = json.loads(guest(sid, 'lewbox-agentd call --file /tmp/agentd-request.json')['stdout'])
    assert result['ok'], result
    return result


def events(sid, conversation, after=0):
    raw = guest(sid, f'lewbox-agentd events --conversation {conversation} --after {after}')['stdout']
    return [json.loads(line) for line in raw.splitlines()]


def wait_events(sid, conversation, predicate):
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        data = events(sid, conversation)
        if predicate(data):
            return data
        time.sleep(.2)
    raise AssertionError(f'Expected daemon events not observed for {conversation}')


def finished(turn):
    return lambda data: any(e['type'] == 'finished' and e['turn'] == turn for e in data)


def prompt(sid, conversation, message, text, **fields):
    return call(sid, {'command': 'prompt', 'conversation': conversation, 'message': message,
        'prompt': text, 'agent': 'claude-code', 'directory': '/root/workspace', **fields})


def start(sid, offline=False):
    guest(sid, ('touch' if offline else 'rm -f') + ' /tmp/lewbox-upload-offline; '
        'nohup python3 /usr/local/lib/lewbox/upload-sink.py > /tmp/upload-sink.log 2>&1 < /dev/null &')
    guest(sid, 'nohup env LEWBOX_AGENTD_CLAUDE_CODE_ADAPTER=/usr/local/bin/lewbox-fake-acp '
        'LEWBOX_AGENTD_CODEX_ADAPTER=/usr/local/bin/lewbox-fake-acp '
        'LEWBOX_AGENTD_CLAUDE_CODE_PACKAGE=none LEWBOX_AGENTD_CODEX_PACKAGE=none '
        'LEWBOX_AGENTD_UPLOAD_INTERVAL=100ms LEWBOX_AGENTD_HEARTBEAT=1s '
        'LEWBOX_AGENTD_ADAPTER_IDLE=1m AGENTD_FAKE_LOG=/root/.lewbox/fake-acp.jsonl '
        'lewbox-agentd serve > /tmp/agentd.log 2>&1 < /dev/null & echo $! > /tmp/agentd.pid')
    guest(sid, 'for i in $(seq 1 60); do lewbox-agentd call \'{"command":"status"}\' >/dev/null 2>&1 '
        '&& curl -fsS http://127.0.0.1:16081/ >/dev/null && exit 0; sleep .2; done; cat /tmp/agentd.log; exit 1')


def experiments(handles):
    builds = sorted((lab.HERE / 'results').glob('agentd-build-*.json'))
    build = next(json.loads(p.read_text()) for p in reversed(builds) if json.loads(p.read_text()).get('passed'))
    lab.REPORT['build'] = build
    lab.start_server('p', handles, allow_shrink=True)
    sid, _ = cold(IMAGE)
    guest(sid, 'test $(cat /sys/class/block/vdb/size) = 41943040; '
        'mkdir -p /usr/local/lib/lewbox /root/workspace /root/.lewbox')
    for name, target in [('lewbox-agentd', '/usr/local/bin/lewbox-agentd'),
                         ('agentd.test', '/usr/local/lib/lewbox/agentd.test')]:
        path = lab.ROOT / 'agentd-bin' / name
        cli('upload', sid, str(path), target)
        guest(sid, f'chmod 755 {target}')
        assert guest(sid, f'sha256sum {target}')['stdout'].split()[0] == build['binaries'][name]['sha256']
    cli('upload', sid, str(lab.HERE / 'agentd-fixture/upload-sink.py'), '/usr/local/lib/lewbox/upload-sink.py')
    write(sid, '/usr/local/bin/lewbox-fake-acp', '#!/bin/sh\nexport AGENTD_FAKE_ADAPTER=1\nexec /usr/local/lib/lewbox/agentd.test\n')
    guest(sid, 'chmod 755 /usr/local/bin/lewbox-fake-acp')
    absent = guest(sid, 'lewbox-agentd call \'{"command":"status"}\'', expected=3)
    lab.record('real_binary_transfer_and_missing_daemon_exit', {'binary_hashes_match': True, 'missing_daemon_exit': absent['exit_code']})
    start(sid, offline=True)
    prompt(sid, 'cnv_basic', 'msg_first', 'hello')
    data = wait_events(sid, 'cnv_basic', lambda d: finished(1)(d) and any(e['type'] == 'title' for e in d))
    assert ''.join(e.get('text', '') for e in data if e['type'] == 'text') == 'Hello'
    assert [e['id'] for e in data] == list(range(1, len(data) + 1))
    last = data[-1]['id']
    assert not events(sid, 'cnv_basic', after=last)
    guest(sid, 'before=$(cat /tmp/agentd.pid); lewbox-agentd serve; kill -0 "$before"')
    lab.record('daemon_cli_events_and_duplicate_start', {'hello_chunks_verified': True, 'event_ids_ordered': True,
        'after_cursor_verified': True, 'duplicate_start_preserves_daemon': True})
    prompt(sid, 'cnv_approval', 'msg_approve', 'approve')
    data = wait_events(sid, 'cnv_approval', lambda d: any(e['type'] == 'approval_request' for e in d))
    approval = next(e['request'] for e in data if e['type'] == 'approval_request')
    call(sid, {'command': 'reply', 'conversation': 'cnv_approval', 'request': approval, 'option': 'allow_once'})
    data = wait_events(sid, 'cnv_approval', finished(1))
    assert any(e['type'] == 'answered' for e in data)
    lab.record('approval_reply_through_real_cli', {'approval_and_finished_events_verified': True})
    prompt(sid, 'cnv_resume', 'msg_running', 'slow')
    wait_events(sid, 'cnv_resume', lambda d: any(e['type'] == 'text' for e in d))
    queued = prompt(sid, 'cnv_resume', 'msg_queued', 'hello', queue=True)
    duplicate = prompt(sid, 'cnv_resume', 'msg_queued', 'hello', queue=True)
    assert queued.get('queued') and duplicate.get('queued')
    state = call(sid, {'command': 'status', 'conversation': 'cnv_resume'})['conversation']
    assert state['queued'] == ['msg_queued']
    assert state['state'] == 'running'
    lab.record('queue_and_request_idempotency', {'one_queued_message': True, 'source_turn_running': state['state'] == 'running'})
    prompt(sid, 'cnv_upload', 'msg_upload', 'hello', upload_url='http://127.0.0.1:16081/uploads',
           upload_token='synthetic-agentd-upload-token')
    wait_events(sid, 'cnv_upload', finished(1))
    state = call(sid, {'command': 'status', 'conversation': 'cnv_upload'})['conversation']
    assert state['uploaded_through'] == 0
    # The real daemon persists its gateway token in conversation metadata. This
    # fixture token is public/synthetic; production secret scrubbing is unresolved.
    token_persisted = guest(sid, "python3 -c 'import json; d=json.load(open(\"/root/.lewbox/agents/cnv_upload/conversation.json\")); "
        "print(bool(d.get(\"upload_token\")))'")['stdout'].strip() == 'True'
    lab.REPORT['synthetic_upload_token_persisted_in_metadata'] = token_persisted
    boot = guest(sid, 'cat /proc/sys/kernel/random/boot_id')['stdout']
    capture = lab.NODE / 'capture'
    capture.mkdir()
    work, image, sealed = prepare_capture(sid, capture / 'agentd.commit')
    assert not list(work.glob('vm_state.bin')) and not list(work.glob('mem_*'))
    payload = sealed.read_bytes()
    assert len(payload) < 512 * 1024**2
    digest = 'sha256:' + hashlib.sha256(payload).hexdigest()
    assert digest == image['lowers'][-1]['digest']
    request('PUT', obj(f'managed-layers/{digest}'), body=payload)
    assert request('GET', obj(f'managed-layers/{digest}')) == payload
    description = json.loads(request('GET', obj(f'images/descriptions/{IMAGE}.json')))
    description['layers'].append({'Managed': {'digest': digest, 'size': len(payload)}})
    assert len(description['layers']) == len(image['lowers'])
    encoded = json.dumps(description, sort_keys=True, separators=(',', ':')).encode()
    saved = 'sha256:' + hashlib.sha256(encoded).hexdigest()
    request('PUT', obj(f'images/descriptions/{saved}.json'), body=encoded)
    assert request('GET', obj(f'images/descriptions/{saved}.json')) == encoded
    lab.record('agentd_disk_only_checkpoint', {'image_digest': saved, 'new_layer_bytes': len(payload),
        'no_guest_ram_snapshot': True, 'source_has_running_turn_and_queued_prompt': True})
    lab.remove_runtime(sid)
    lab.stop_server()
    shutil.rmtree(lab.NODE / 'p')
    shutil.rmtree(capture)
    lab.start_server('r', handles, allow_shrink=True)
    restored, _ = cold(saved)
    assert guest(restored, 'cat /proc/sys/kernel/random/boot_id')['stdout'] != boot
    assert guest(restored, 'sha256sum /usr/local/bin/lewbox-agentd')['stdout'].split()[0] == build['binaries']['lewbox-agentd']['sha256']
    guest(restored, 'lewbox-agentd call \'{"command":"status"}\'', expected=3)
    start(restored)
    data = wait_events(restored, 'cnv_resume', finished(2))
    ends = [e for e in data if e['type'] == 'finished']
    assert any(e['turn'] == 1 and e['stop_reason'] == 'interrupted' for e in ends)
    assert any(e['turn'] == 2 and e['message'] == 'msg_queued' and e['stop_reason'] == 'end_turn' for e in ends)
    assert sum(e['turn'] == 2 for e in ends) == 1
    assert [e['id'] for e in data] == list(range(1, len(data) + 1))
    lab.record('cold_restore_interrupts_old_turn_and_drains_queue', {'new_kernel_boot_id': True,
        'old_turn_interrupted': True, 'queued_turn_completed_once': sum(e['turn'] == 2 for e in ends) == 1,
        'event_ids_continue_in_order': True, 'binary_preserved_without_reupload': True})
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        state = call(restored, {'command': 'status', 'conversation': 'cnv_upload'})['conversation']
        if state['uploaded_through'] >= state['last_event']:
            break
        time.sleep(.2)
    assert state['uploaded_through'] >= state['last_event'] > 0, state
    uploaded = [json.loads(l) for l in guest(restored, 'cat /root/.lewbox/upload-fixture/accepted.jsonl')['stdout'].splitlines()]
    assert all(x['authorization_valid'] for x in uploaded)
    assert any(x['upload']['conversation'] == 'cnv_upload' and x['upload']['through'] >= state['last_event'] for x in uploaded)
    lab.record('pending_upload_replays_after_cold_restore', {'accepted_by_local_receiver': True,
        'uploaded_through': state['uploaded_through'], 'last_event': state['last_event'],
        'synthetic_authorization_verified': True, 'real_gateway_tested': False})
    prompt(restored, 'cnv_basic', 'msg_second', 'hello')
    data = wait_events(restored, 'cnv_basic', finished(2))
    assert any(e['type'] == 'started' and e['turn'] == 2 for e in data)
    lab.record('existing_conversation_accepts_prompt_after_cold_restore', {'second_turn_finished': True})
    lab.remove_runtime(restored)
    lab.REPORT['execution_passed'] = True
    lab.save()


if __name__ == '__main__':
    lab.main(experiments)
