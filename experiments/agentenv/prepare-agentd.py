#!/usr/bin/env python3
"""Build unchanged legacy agentd and its existing scripted ACP test adapter.

Downloads the module's Go version into the lab only, verifies the official archive
checksum, and uses isolated HOME/cache/temp paths and a minimal child environment.
"""
import hashlib
import atexit
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import time
import urllib.request

HERE = Path(__file__).resolve().parent
ROOT = Path(os.environ['LAB_ROOT'])
SOURCE = Path(os.environ.get('LEWBOX_GATEWAY_PATH', '/home/homelab/dev/legacy/lewbox-gateway'))
VERSION = 'go1.27.1'
RUN = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
OUTPUT = HERE / 'results' / f'agentd-build-{RUN}.json'
REPORT = {'source': str(SOURCE), 'go_version': VERSION, 'upstream_source_modified': False}


def save():
    OUTPUT.write_text(json.dumps(REPORT, indent=2) + '\n')


def main():
    REPORT['gateway_commit'] = subprocess.check_output(['git', '-C', str(SOURCE), 'rev-parse', 'HEAD'], text=True).strip()
    assert not subprocess.check_output(['git', '-C', str(SOURCE), 'status', '--porcelain'], text=True), 'Use an unchanged source checkout'
    releases = json.load(urllib.request.urlopen('https://go.dev/dl/?mode=json&include=all', timeout=60))
    release = next(r for r in releases if r['version'] == VERSION)
    asset = next(a for a in release['files'] if a['os'] == 'linux' and a['arch'] == 'amd64' and a['kind'] == 'archive')
    REPORT['toolchain'] = {**asset, 'metadata_url': 'https://go.dev/dl/?mode=json&include=all'}
    archive = ROOT / 'downloads' / asset['filename']
    archive.parent.mkdir(exist_ok=True)
    if not archive.exists():
        with urllib.request.urlopen('https://go.dev/dl/' + asset['filename'], timeout=60) as response, archive.open('xb') as target:
            shutil.copyfileobj(response, target)
    with archive.open('rb') as stream:
        assert hashlib.file_digest(stream, 'sha256').hexdigest() == asset['sha256'], 'Go archive checksum mismatch'
    toolchain = ROOT / 'toolchains' / VERSION
    if not toolchain.exists():
        toolchain.mkdir(parents=True)
        with tarfile.open(archive) as tar:
            tar.extractall(toolchain, filter='data')
    binary = toolchain / 'go/bin/go'
    paths = {name: ROOT / ('agentd-' + name.lower()) for name in ('HOME', 'GOCACHE', 'GOMODCACHE', 'GOPATH', 'TMPDIR')}
    for path in paths.values():
        path.mkdir(exist_ok=True)
    # Go's t.TempDir includes test names; a deep lab TMPDIR exceeds the Unix
    # socket pathname limit used by the unchanged daemon tests.
    paths['TMPDIR'] = Path(tempfile.mkdtemp(prefix='l'))
    atexit.register(shutil.rmtree, paths['TMPDIR'])
    env = {**{name: str(value) for name, value in paths.items()}, 'PATH': f'{binary.parent}:/usr/bin:/bin',
           'LANG': 'C.UTF-8', 'CGO_ENABLED': '0', 'GOTOOLCHAIN': 'local', 'GOENV': 'off',
           'GOMAXPROCS': '2', 'GOFLAGS': '-mod=readonly'}
    target = ROOT / 'agentd-bin'
    target.mkdir(exist_ok=True)
    REPORT['compiler'] = subprocess.check_output([str(binary), 'version'], env=env, text=True).strip()
    logfile = HERE / 'results' / f'agentd-unit-{RUN}.jsonl'
    started = time.monotonic()
    with logfile.open('w') as log:
        result = subprocess.run([str(binary), 'test', '-json', '-count=1', '-timeout=180s',
            './internal/agentd', './cmd/lewbox-agentd'], cwd=SOURCE, env=env, stdout=log, stderr=log, timeout=240)
    events = []
    for line in logfile.read_text().splitlines():
        try:
            events.append(json.loads(line))
        except ValueError:
            pass
    REPORT['unit_tests'] = {'exit_code': result.returncode, 'log': logfile.name,
        'passed_tests': sum(e.get('Action') == 'pass' and bool(e.get('Test')) for e in events),
        'failed_tests': [e.get('Test', e.get('Package')) for e in events if e.get('Action') == 'fail'],
        'elapsed_seconds': round(time.monotonic() - started, 4)}
    save()
    assert result.returncode == 0, f'Existing daemon tests failed; see {logfile.name}'
    for command in (
        ['build', '-trimpath', '-ldflags=-s -w', '-o', str(target / 'lewbox-agentd'), './cmd/lewbox-agentd'],
        ['test', '-c', '-o', str(target / 'agentd.test'), './internal/agentd'],
    ):
        subprocess.run([str(binary), *command], cwd=SOURCE, env=env, check=True, timeout=240)
    REPORT['binaries'] = {}
    for name in ('lewbox-agentd', 'agentd.test'):
        path = target / name
        with path.open('rb') as stream:
            REPORT['binaries'][name] = {'bytes': path.stat().st_size, 'sha256': hashlib.file_digest(stream, 'sha256').hexdigest()}
    REPORT['passed'] = True
    save()
    print(json.dumps(REPORT), flush=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        REPORT.update(passed=False, error=str(error))
        save()
        raise
