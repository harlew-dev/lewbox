"""Minimal SigV4 client for the loopback-only S3 fixture (no external accounts)."""
import datetime
import hashlib
import hmac
import json
import os
from pathlib import Path
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

ROOT = Path(os.environ["LAB_ROOT"])
BUCKET = "lewbox-agentenv-lab"


def credentials():
    return json.loads((ROOT / "local-s3-credentials.json").read_text())


def request(method, path, query=None, body=b"", sink=None):
    creds = credentials()
    host = "127.0.0.1:19000"
    now = datetime.datetime.now(datetime.timezone.utc)
    date, stamp = now.strftime("%Y%m%d"), now.strftime("%Y%m%dT%H%M%SZ")
    payload_hash = hashlib.sha256(body).hexdigest()
    query_string = urllib.parse.urlencode(sorted((query or {}).items()))
    canonical_headers = f"host:{host}\nx-amz-content-sha256:{payload_hash}\nx-amz-date:{stamp}\n"
    signed = "host;x-amz-content-sha256;x-amz-date"
    canonical = "\n".join([method, path, query_string, canonical_headers, signed, payload_hash])
    scope = f"{date}/us-east-1/s3/aws4_request"
    to_sign = f"AWS4-HMAC-SHA256\n{stamp}\n{scope}\n{hashlib.sha256(canonical.encode()).hexdigest()}"
    def sign(key, value):
        return hmac.new(key, value.encode(), hashlib.sha256).digest()
    key = sign(sign(sign(sign(("AWS4" + creds['secret']).encode(), date), "us-east-1"), "s3"), "aws4_request")
    signature = hmac.new(key, to_sign.encode(), hashlib.sha256).hexdigest()
    auth = f"AWS4-HMAC-SHA256 Credential={creds['access']}/{scope}, SignedHeaders={signed}, Signature={signature}"
    req = urllib.request.Request(f"http://{host}{path}" + ("?" + query_string if query_string else ""),
        method=method, data=body if method in ("PUT", "POST") else None,
        headers={"Authorization": auth, "X-Amz-Date": stamp, "X-Amz-Content-Sha256": payload_hash})
    with urllib.request.urlopen(req, timeout=60) as response:
        if sink is not None:
            while chunk := response.read(1024 * 1024):
                sink.write(chunk)
            return None
        return response.read()


def objects(prefix=None):
    results = []
    query = {"list-type": "2"}
    if prefix is not None:
        query['prefix'] = prefix
    ns = {"s": "http://s3.amazonaws.com/doc/2006-03-01/"}
    while True:
        xml = ET.fromstring(request("GET", f"/{BUCKET}", query))
        for content in xml.findall("s:Contents", ns):
            results.append({"key": content.findtext("s:Key", namespaces=ns),
                            "bytes": int(content.findtext("s:Size", namespaces=ns))})
        token = xml.findtext("s:NextContinuationToken", namespaces=ns)
        if not token:
            return results
        query["continuation-token"] = token
