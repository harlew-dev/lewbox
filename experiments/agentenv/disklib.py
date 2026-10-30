#!/usr/bin/env python3
"""Private disk-sealing helpers for disposable feasibility experiments only.

No supported product API, locking, automatic thaw/rollback, or RAM capture.
The caller owns a single-VM fixture, publication, recovery and cleanup.
"""
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import struct

from smoke import guest


def fc(work, method, route, body=None):
    connection = http.client.HTTPConnection('localhost', timeout=20)
    connection.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    connection.sock.settimeout(20)
    connection.sock.connect(str(work / 'firecracker.socket'))
    try:
        connection.request(method, route, json.dumps(body) if body is not None else None,
                           {'Content-Type': 'application/json'})
        response = connection.getresponse()
        data = response.read()
        assert response.status in (200, 204), (route, response.status, data[:300])
        return json.loads(data) if data else None
    finally:
        connection.close()


def daemon(body):
    def receive(connection, count):
        data = bytearray()
        while len(data) < count:
            chunk = connection.recv(count - len(data))
            assert chunk, 'Unexpected ublk daemon EOF'
            data.extend(chunk)
        return bytes(data)
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(120)
        connection.connect(str(Path(os.environ['AENV_RUNTIME_PATH']) / 'ublk-daemon.sock'))
        payload = json.dumps(body).encode()
        connection.sendall(struct.pack('!I', len(payload)) + payload)
        length = struct.unpack('!I', receive(connection, 4))[0]
        assert length < 16 * 1024**2
        result = json.loads(receive(connection, length))
    assert result['status'] == 'restack_snapshot_created', result
    return result


def workspace():
    home = Path(os.environ['AENV_HOME_PATH'])
    candidates = list(home.glob('firecracker-work/*/user-rootfs'))
    assert len(candidates) == 1, candidates
    return candidates[0].parent


def prepare_capture(sid, sealed, work=None):
    work = workspace() if work is None else work
    guest(sid, 'sync; fsfreeze -f /')
    fc(work, 'PATCH', '/vm', {'state': 'Paused'})
    root_device = (work / 'user-rootfs').resolve()
    assert root_device.name.startswith('ublkb'), root_device
    sealed = Path(sealed)
    response = daemon({'kind': 'restack_snapshot', 'dev_id': int(root_device.name[5:]),
                       'output_layer_path': str(sealed)})
    assert sealed.is_file()
    descriptor = response.get('descriptor')
    if descriptor is None:
        # The released daemon permits an absent descriptor; the upstream caller
        # likewise falls back to reading the sealed file rather than requiring it.
        with sealed.open('rb') as stream:
            descriptor = {'digest': 'sha256:' + hashlib.file_digest(stream, 'sha256').hexdigest(),
                          'size': sealed.stat().st_size}
    image_path = work / 'overlaybd/image.json'
    image = json.loads(image_path.read_text())
    for lower in image.get('lowers', []):
        if lower.get('file') and not Path(lower['file']).is_absolute():
            lower['file'] = str((image_path.parent / lower['file']).resolve())
    image.setdefault('lowers', []).append({'file': str(sealed), 'digest': descriptor['digest'], 'size': descriptor['size']})
    # Mirror the upstream caller's config rewrite so a failed publication can resume safely.
    temporary = image_path.with_suffix('.new')
    temporary.write_text(json.dumps(image))
    temporary.replace(image_path)
    return work, image, sealed
