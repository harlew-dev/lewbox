"""Guest-loopback receiver for synthetic agentd uploads, not a real gateway."""
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from pathlib import Path

ROOT = Path('/root/.lewbox/upload-fixture')
ROOT.mkdir(parents=True, exist_ok=True)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        self.send_response(200)
        self.end_headers()

    def do_POST(self):
        size = int(self.headers.get('Content-Length', '0'))
        assert 0 < size <= 2 * 1024**2
        body = json.loads(self.rfile.read(size))
        valid = self.headers.get('Authorization') == 'Bearer synthetic-agentd-upload-token'
        status = 503 if Path('/tmp/lewbox-upload-offline').exists() else (200 if valid else 401)
        if status == 200:
            with (ROOT / 'accepted.jsonl').open('a') as stream:
                stream.write(json.dumps({'authorization_valid': valid, 'upload': body}) + '\n')
        self.send_response(status)
        self.end_headers()


HTTPServer(('127.0.0.1', 16081), Handler).serve_forever()
