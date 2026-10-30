#!/usr/bin/env python3
"""Configure only this lab's CLI, without printing its API key."""
import json
import os
from pathlib import Path
import time
import urllib.request

for attempt in range(60):
    try:
        urllib.request.urlopen("http://127.0.0.1:18080/health", timeout=1).close()
        break
    except OSError:
        time.sleep(0.5)
else:
    raise SystemExit("Lab API was not healthy within 30 seconds")

key = (Path(os.environ["AENV_HOME_PATH"]) / "secrets/api-key").read_text().strip()
target = Path(os.environ["XDG_CONFIG_HOME"]) / "aenv/credentials"
target.parent.mkdir(parents=True, exist_ok=True)
target.touch(mode=0o600, exist_ok=True)
target.chmod(0o600)
target.write_text('url = "http://127.0.0.1:18080"\napi_key = ' + json.dumps(key) + '\n')
print("Configured isolated lab CLI credentials")
