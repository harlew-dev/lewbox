#!/usr/bin/env python3
"""Start the pinned Silo test store on loopback with disposable credentials."""
import json
import os
from pathlib import Path
import secrets

root = Path(os.environ["LAB_ROOT"])
path = root / "local-s3-credentials.json"
if not path.exists():
    path.touch(mode=0o600)
    path.write_text(json.dumps({"access": "lewbox-local-lab", "secret": secrets.token_hex(24)}))
creds = json.loads(path.read_text())
env = dict(os.environ, MINIO_ROOT_USER=creds['access'], MINIO_ROOT_PASSWORD=creds['secret'], MINIO_BROWSER="off")
binary = str(root / "silo/silo")
os.execve(binary, [binary, "server", str(root / "s3-data"), "--address", "127.0.0.1:19000",
                  "--console-address", "127.0.0.1:19001"], env)
