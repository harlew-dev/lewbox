#!/usr/bin/env python3
"""Download checksum-pinned lab binaries; no system installer or source edits."""
import hashlib
import os
from pathlib import Path
import tarfile
import urllib.request

root = Path(os.environ.get("LAB_ROOT", "/home/homelab/.local/share/lewbox-agentenv-lab"))
assets = [
    ("https://github.com/kvcache-ai/AgentENV/releases/download/v0.2.4/aenv-linux-x86_64.tar.gz",
     "412b9c26e259dc1f4dce69ff1ee4763a506ab4debbc315b20057215ea3cdc2c4", "cli"),
    ("https://github.com/kvcache-ai/AgentENV/releases/download/v0.2.4/aenv-server-linux-x86_64.tar.gz",
     "e99e4221d32104c989fda5ca40e80a5b00d13165569b0d11eba32d42fa7b10e0", "server"),
    # Same S3 test-server release selected by AgentENV's own test fixture.
    ("https://github.com/pgsty/silo/releases/download/RELEASE.2026-09-16T00-00-00Z/silo_20260916000000.0.0_linux_amd64.tar.gz",
     "381e745510a8fb64323d7bb3207f95984b7f4ed826f4fcad318f97683c420c73", "silo"),
]
(root / "downloads").mkdir(parents=True, exist_ok=True)
for url, expected, directory in assets:
    archive = root / "downloads" / url.rsplit("/", 1)[1]
    if not archive.exists():
        part = archive.with_suffix(".part")
        urllib.request.urlretrieve(url, part)
        part.rename(archive)
    with archive.open("rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    if digest != expected:
        raise SystemExit(f"Checksum mismatch: {archive}")
    target = root / directory
    target.mkdir(exist_ok=True)
    if not any(target.iterdir()):
        with tarfile.open(archive) as tar:
            tar.extractall(target, filter="data")
    print(f"Verified {archive.name}")
