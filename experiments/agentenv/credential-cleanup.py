#!/usr/bin/env python3
"""Check whether legacy-style credential unlinking removes exported disk bytes.

Synthetic canary only. Sealed layers stay local and are discarded; no credentials
or guest memory are uploaded. An absent canary is only a fixture result, not a
general secure-erasure proof.
"""
import json
import mmap
import os
from pathlib import Path
import time

import lewbox as lab
from disklib import prepare_capture
from smoke import api, guest

RUN = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
CANARY = 'SYNTHETIC-LEWBOX-IMPORTED-CREDENTIAL-ERASURE-PROBE-20261030'
lab.RUN, lab.PREFIX = RUN, f'credential-cleanup-{RUN}/'
lab.OUTPUT = lab.HERE / 'results' / f'credential-cleanup-{RUN}.json'
lab.REPORT = {'release': 'v0.2.4', 'prefix': lab.PREFIX, 'tests': [], 'storage': [],
              'node_backing_bytes': 75_000_000_000, 'real_credentials_used': False,
              'scope': 'Synthetic file unlink and optional fstrim; raw disk layers only, no publication'}


def experiments(handles):
    lab.start_server('p', handles, allow_shrink=True)
    capture = lab.NODE / 'capture'
    capture.mkdir()
    for trim in (False, True):
        data, _ = api('POST', '/sandboxes-cold', {'image': 'alpine:3.22', 'cpuCount': 1,
            'memoryMB': 512, 'diskSizeMB': 20480, 'timeout': 600,
            'autoPause': False, 'secure': True}, expected=201)
        sid = data['sandboxID']
        guest(sid, 'apk add --no-cache util-linux >/dev/null; '
            'mkdir -p /root/.config/lewbox /root/.codex; '
            f"yes '{CANARY}' | head -c 8192 > /root/.config/lewbox/env; "
            'cp /root/.config/lewbox/env /root/.codex/auth.json; '
            'chmod 600 /root/.config/lewbox/env /root/.codex/auth.json; sync; '
            'rm -f /root/.config/lewbox/env /root/.codex/auth.json; sync; '
            'test ! -e /root/.config/lewbox/env; test ! -e /root/.codex/auth.json')
        trim_result = None
        if trim:
            trim_result = guest(sid, 'set +e; fstrim -v /; echo TRIM_EXIT=$?')
        work, image, sealed = prepare_capture(sid, capture / f'trim-{trim}.commit')
        found = []
        for lower in image['lowers']:
            if lower.get('file'):
                path = Path(lower['file'])
                with path.open('rb') as stream, mmap.mmap(stream.fileno(), 0, access=mmap.ACCESS_READ) as mapped:
                    offset = mapped.find(CANARY.encode())
                    if offset >= 0:
                        found.append({'layer': path.name, 'offset': offset, 'layer_bytes': path.stat().st_size})
        assert not list(work.glob('vm_state.bin')) and not list(work.glob('mem_*'))
        lab.record('unlinked_credentials_absent_from_raw_layers' + ('_with_fstrim' if trim else ''),
            {'paths_absent_in_guest': True, 'canary_found_in_layers': found,
             'fstrim_result': trim_result, 'sealed_layer_bytes': sealed.stat().st_size,
             'no_guest_ram_capture': True, 'published': False}, outcome='blocked' if found else 'passed')
        lab.remove_runtime(sid)
    lab.REPORT['execution_passed'] = True
    lab.save()


if __name__ == '__main__':
    lab.main(experiments)
