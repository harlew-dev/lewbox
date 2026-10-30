#!/usr/bin/env python3
"""Build the unchanged legacy desktop, then test a 20 GiB disk-only restart.

Uses synthetic files, no agent login, model call or personal credential. The
private disk-sealing adapter remains a feasibility prototype, not product code.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import time
import urllib.parse

import lewbox as lab
from disklib import prepare_capture
from s3lib import BUCKET, objects, request
from smoke import api, guest, pty_check

RUN = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
LEGACY = Path(os.environ.get('LEWBOX_LEGACY_PATH', '/home/homelab/dev/legacy/lewbox'))
REUSE_IMAGE = os.environ.get('LEWBOX_WORKLOAD_IMAGE')
lab.RUN, lab.PREFIX = RUN, os.environ.get('LEWBOX_WORKLOAD_PREFIX', f'workload-{RUN}/')
lab.OUTPUT = lab.HERE / 'results' / f'workload-{RUN}.json'
lab.REPORT = {'release': 'v0.2.4', 'prefix': lab.PREFIX, 'tests': [], 'storage': [],
              'node_backing_bytes': 75_000_000_000, 'real_r2_tested': False,
              'scope': 'Legacy desktop, one VM, 2 CPUs/4096 MiB/20 GiB; private disk-only adapter',
              'agentd_implementation_available': (Path(os.environ.get('LEWBOX_GATEWAY_PATH',
                  '/home/homelab/dev/legacy/lewbox-gateway')) / 'cmd/lewbox-agentd/main.go').is_file(),
              'authenticated_agent_calls_tested': False}


def object_path(key):
    return f'/{BUCKET}/' + urllib.parse.quote(lab.PREFIX + key, safe='/')


def cold(image, disk=20480):
    result, seconds = api('POST', '/sandboxes-cold', {'image': image, 'cpuCount': 2,
        'memoryMB': 4096, 'diskSizeMB': disk, 'timeout': 3600,
        'autoPause': False, 'secure': True}, expected=201)
    return result['sandboxID'], seconds


def hydrate_for_resize(description):
    """Private cache rewrite: C++ resize cannot open Rust-only remote references.

    Download and hash-verify every managed layer before replacing that isolated
    source config with local paths. No live runtime references may exist here.
    """
    expected = [x['Managed']['digest'] for x in description['layers']]
    cache = Path(os.environ['AENV_HOME_PATH']) / 'image-cache'
    matches = []
    for candidate in cache.rglob('*.json'):
        try:
            data = json.loads(candidate.read_text())
        except (ValueError, UnicodeError):
            continue
        if isinstance(data, dict) and [x.get('digest') for x in data.get('lowers', [])] == expected:
            matches.append((candidate, data))
    assert len(matches) == 1, 'Expected one isolated published-image source config'
    directory = lab.NODE / 'hydrated'
    directory.mkdir()
    lowers = []
    for reference in description['layers']:
        layer = reference['Managed']
        path = directory / layer['digest'].replace(':', '-')
        with path.open('xb') as stream:
            request('GET', object_path(f"managed-layers/{layer['digest']}"), sink=stream)
        with path.open('rb') as stream:
            assert 'sha256:' + hashlib.file_digest(stream, 'sha256').hexdigest() == layer['digest']
        assert path.stat().st_size == layer['size']
        lowers.append({'file': str(path), 'digest': layer['digest'], 'size': layer['size']})
    candidate, config = matches[0]
    config['lowers'] = lowers
    temporary = candidate.with_suffix('.new')
    temporary.write_text(json.dumps(config))
    temporary.replace(candidate)
    lab.record('prefetch_image_layers_for_cpp_resize', {'layers': len(lowers),
        'downloaded_bytes': sum(x['size'] for x in lowers), 'private_cache_rewrite': True})


def start_desktop(sid):
    # Cold-create does not execute image CMD on the tested release.
    guest(sid, 'nohup env DISPLAY=:0 DESKTOP_SIZE=1280x800 XDG_RUNTIME_DIR=/tmp/runtime-root '
          '/usr/local/bin/start-desktop > /tmp/start-desktop.log 2>&1 < /dev/null &')
    return guest(sid, 'export DISPLAY=:0; '
        'for i in $(seq 1 90); do '
        'if xdpyinfo >/dev/null 2>&1 && curl -fsS http://127.0.0.1:6080/ >/tmp/desktop.html; then break; fi; '
        'sleep 1; done; xdpyinfo >/dev/null; test -s /tmp/desktop.html; '
        'pgrep -x xfce4-session >/dev/null; pgrep -x sshd >/dev/null; '
        'scrot /root/workspace/desktop.png; test -s /root/workspace/desktop.png; '
        'stat -c SCREENSHOT_BYTES=%s /root/workspace/desktop.png; '
        'curl -fsS -H "Content-Type: application/json" -d \'{"action":"screenshot"}\' '
        'http://127.0.0.1:6081/computer-use > /tmp/computer-use.json; '
        'python3 -c \'import json; d=json.load(open("/tmp/computer-use.json")); '
        's=json.dumps(d); assert "iVBORw0KGgo" in s or "/9j/" in s; '
        'print("COMPUTER_USE_IMAGE_RECEIVED=true")\'')['stdout']


def fingerprint(sid):
    return guest(sid, "sha256sum /root/workspace/project/server.js /etc/ssh/ssh_host_ed25519_key.pub "
        "/etc/lewbox-workload-marker; stat -c '%u:%g:%a' /etc/lewbox-workload-marker; "
        'readlink /root/workspace/marker-link')['stdout']


def experiments(handles):
    lab.REPORT['legacy_commit'] = lab.run('git', '-C', str(LEGACY), 'rev-parse', 'HEAD')
    source = LEGACY / 'docs/examples/templates/desktop.Dockerfile'
    payload = source.read_bytes()
    lab.REPORT['dockerfile_sha256'] = hashlib.sha256(payload).hexdigest()
    context = lab.ROOT / f'workload-context-{RUN}'
    context.mkdir()
    (context / 'Dockerfile').write_bytes(payload)
    shutil.copyfile(source, lab.HERE / 'results' / f'workload-{RUN}.Dockerfile')
    lab.save()
    temporary = lab.NODE / 'tmp'
    temporary.mkdir()
    os.environ['TMPDIR'] = str(temporary)
    lab.start_server('p', handles, allow_shrink=True)
    log_path = lab.HERE / 'results' / f'workload-{RUN}-build.log'
    started = time.monotonic()
    if REUSE_IMAGE:
        assert re.fullmatch(r'sha256:[0-9a-f]{64}', REUSE_IMAGE)
        digest = REUSE_IMAGE
    else:
        with log_path.open('w') as log:
            process = subprocess.Popen(['aenv', 'build', '--image', '--name', f'legacy-desktop-{RUN}',
                '--timeout', '1200', '--progress', 'plain', str(context)], stdout=log, stderr=subprocess.STDOUT)
            handles.append(process)
            deadline = time.monotonic() + 1800
            while process.poll() is None:
                assert time.monotonic() < deadline, 'Desktop image build exceeded deadline'
                assert shutil.disk_usage(lab.ROOT).free > 12 * 1024**3, 'Host reserve reached'
                time.sleep(1)
        if process.returncode:
            lab.record('unchanged_legacy_desktop_build', {'exit_code': process.returncode,
                'log': log_path.name, 'elapsed_seconds': round(time.monotonic() - started, 4)}, outcome='blocked')
            raise RuntimeError(f'Legacy Dockerfile build failed; see {log_path.name}')
        digest = next(line.strip() for line in reversed(log_path.read_text().splitlines())
                      if re.fullmatch(r'sha256:[0-9a-f]{64}', line.strip()))
    description_bytes = request('GET', object_path(f'images/descriptions/{digest}.json'))
    assert 'sha256:' + hashlib.sha256(description_bytes).hexdigest() == digest
    description = json.loads(description_bytes)
    lab.record('reuse_previously_built_desktop_image' if REUSE_IMAGE else 'unchanged_legacy_desktop_build',
        {'image_digest': digest, 'log': None if REUSE_IMAGE else log_path.name,
        'elapsed_seconds': round(time.monotonic() - started, 4),
        'published_layer_bytes': sum(x['Managed']['size'] for x in description['layers'] if 'Managed' in x)})
    if os.environ.get('LEWBOX_WORKLOAD_HYDRATE') == '1':
        control, seconds = cold(digest, disk=65536)
        versions = guest(control, 'node --version; claude --version; codex --version; chromium --version')['stdout']
        lab.remove_runtime(control)
        lab.record('original_remote_image_64g_control', {'create_api_seconds': seconds, 'versions': versions})
        hydrate_for_resize(description)
    work_base = Path(os.environ['AENV_HOME_PATH']) / 'firecracker-work'
    before_work = set(work_base.glob('*/user-rootfs'))
    sid, seconds = cold(digest)
    new_work = set(work_base.glob('*/user-rootfs')) - before_work
    assert len(new_work) == 1, 'Desktop runtime must have one newly allocated work directory'
    desktop_work = new_work.pop().parent
    guest(sid, 'test $(cat /sys/class/block/vdb/size) = 41943040; '
        'command -v fsfreeze; node --version; claude --version; codex --version; chromium --version')
    server_js = 'require("http").createServer((q,s)=>s.end("lewbox-preview\\n")).listen(3000,"127.0.0.1");'
    guest(sid, 'mkdir -p /root/workspace/project; '
        "printf 'owned-marker\\n' > /etc/lewbox-workload-marker; "
        'chown 1234:2345 /etc/lewbox-workload-marker; chmod 640 /etc/lewbox-workload-marker; '
        'ln -s /etc/lewbox-workload-marker /root/workspace/marker-link; '
        f"printf '%s\\n' {shlex.quote(server_js)} > /root/workspace/project/server.js")
    desktop = start_desktop(sid)
    versions = guest(sid, 'node --version; claude --version; codex --version; chromium --version')['stdout']
    lab.record('desktop_services_on_20g_cold_guest', {'create_api_seconds': seconds,
        'services_and_screenshot': desktop, 'versions': versions, 'startup_invoked_by_adapter': True})
    lab.record('desktop_interactive_pty', pty_check(sid))
    guest(sid, 'nohup node /root/workspace/project/server.js > /tmp/preview.log 2>&1 < /dev/null &')
    preview = guest(sid, 'for i in $(seq 1 20); do curl -fsS http://127.0.0.1:3000/ && exit 0; sleep 1; done; exit 1')['stdout']
    assert preview.strip() == 'lewbox-preview'
    lab.record('node_preview_inside_guest', {'response': preview, 'public_gateway_proxy_tested': False})
    original = fingerprint(sid)
    boot = guest(sid, 'cat /proc/sys/kernel/random/boot_id')['stdout']
    capture_dir = lab.NODE / 'capture'
    capture_dir.mkdir()
    work, image, sealed = prepare_capture(sid, capture_dir / 'desktop.commit', work=desktop_work)
    assert not list(work.glob('vm_state.bin')) and not list(work.glob('mem_*'))
    lower = image['lowers'][-1]
    assert sealed.stat().st_size < 512 * 1024**2, 'Small-fixture uploader refuses a large layer'
    layer = sealed.read_bytes()
    layer_digest = 'sha256:' + hashlib.sha256(layer).hexdigest()
    assert lower['digest'] == layer_digest
    request('PUT', object_path(f'managed-layers/{layer_digest}'), body=layer)
    assert request('GET', object_path(f'managed-layers/{layer_digest}')) == layer
    description['layers'].append({'Managed': {'digest': layer_digest, 'size': len(layer)}})
    encoded = json.dumps(description, sort_keys=True, separators=(',', ':')).encode()
    saved = 'sha256:' + hashlib.sha256(encoded).hexdigest()
    request('PUT', object_path(f'images/descriptions/{saved}.json'), body=encoded)
    assert request('GET', object_path(f'images/descriptions/{saved}.json')) == encoded
    # Whole lower chain from the original immutable image plus the sealed upper.
    assert len(image['lowers']) == len(description['layers'])
    lab.record('desktop_disk_only_publication', {'image_digest': saved,
        'new_disk_layer_bytes': len(layer), 'original_image_layers_reused': len(description['layers']) - 1,
        'no_guest_memory_artifacts': True})
    lab.remove_runtime(sid)
    lab.stop_server()
    shutil.rmtree(lab.NODE / 'p')
    shutil.rmtree(capture_dir)
    if (lab.NODE / 'hydrated').exists():
        shutil.rmtree(lab.NODE / 'hydrated')
    lab.start_server('r', handles, allow_shrink=True)
    restored, seconds = cold(saved)
    assert guest(restored, 'cat /proc/sys/kernel/random/boot_id')['stdout'] != boot
    assert fingerprint(restored) == original
    guest(restored, 'test $(cat /sys/class/block/vdb/size) = 41943040; '
        'test ! -e /tmp/computer-use.json || rm /tmp/computer-use.json')
    after = start_desktop(restored)
    lab.record('desktop_fresh_state_disk_only_cold_restore', {'create_api_seconds': seconds,
        'hashes_metadata_and_ssh_host_key_match': True, 'new_kernel_boot_id': True,
        'desktop_services_and_screenshot': after, 'disk_bytes': 20 * 1024**3})
    lab.remove_runtime(restored)
    lab.REPORT['execution_passed'] = True
    lab.REPORT['remote_object_bytes'] = sum(x['bytes'] for x in objects(lab.PREFIX))
    lab.save()


if __name__ == '__main__':
    lab.main(experiments)
