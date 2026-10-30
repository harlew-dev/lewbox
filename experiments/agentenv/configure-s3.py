#!/usr/bin/env python3
"""Create separate publisher/restore configs using the local S3 test store."""
import os
from pathlib import Path
import urllib.error
from s3lib import BUCKET, credentials, request

root = Path(os.environ['LAB_ROOT'])
creds = credentials()
try:
    request("PUT", f"/{BUCKET}")
except urllib.error.HTTPError as error:
    if error.code != 409 or b"BucketAlreadyOwnedByYou" not in error.read():
        raise
for role in ["publisher", "restore"]:
    # Firecracker's Unix API socket must fit Linux's 108-byte sockaddr_un.
    home = root / {"publisher": "op", "restore": "or"}[role]
    home.mkdir(exist_ok=True)
    config = (root / 'config.toml').read_text().replace(str(root / 'state'), str(home))
    config = config.replace('repository_backend = "posix_fs"', 'repository_backend = "oss"')
    config += f'''\n[backend.oss]
endpoint = "http://127.0.0.1:19000"
bucket = "{BUCKET}"
region = "us-east-1"
prefix = "lab/"
addressing_style = "path"
access_key_id = "{creds['access']}"
access_key_secret = "{creds['secret']}"
cache_max_size_gb = 2
'''
    target = root / f"config-oss-{role}.toml"
    target.touch(mode=0o600)
    target.write_text(config)
print("Created isolated OSS publisher and fresh-restore configurations")
