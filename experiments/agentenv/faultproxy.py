"""Loopback-only S3 proxy that can reject managed-layer PUTs after preflight.

Synthetic lab traffic only. Never logs headers, credentials or object bodies.
"""
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import threading


class StoreProxy:
    def __init__(self):
        self.reject_uploads = threading.Event()
        self.failures = 0
        self.allowed_attempts = 0
        self.successful_puts_before_failure = 0
        self.lock = threading.Lock()
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def forward(self):
                # The released client uses Content-Length for these fixtures.
                if self.headers.get('Transfer-Encoding'):
                    self.send_error(501, 'Lab proxy does not support streaming transfer encoding')
                    return
                size = int(self.headers.get('Content-Length', '0'))
                if size > 512 * 1024**2:
                    self.send_error(413, 'Lab proxy small-fixture bound')
                    return
                body = self.rfile.read(size)
                selected = owner.reject_uploads.is_set() and self.command == 'PUT' and '/managed-layers/' in self.path
                reject = False
                if selected:
                    with owner.lock:
                        # Let one layer commit, then fail all later layer PUTs.
                        reject = owner.allowed_attempts > 0
                        owner.allowed_attempts += 1
                        if reject:
                            owner.failures += 1
                if reject:
                    payload = b'<Error><Code>InternalError</Code><Message>Synthetic layer upload failure</Message></Error>'
                    self.send_response(500)
                    self.send_header('Content-Type', 'application/xml')
                    self.send_header('Content-Length', str(len(payload)))
                    self.end_headers()
                    self.wfile.write(payload)
                    return
                connection = http.client.HTTPConnection('127.0.0.1', 19000, timeout=120)
                try:
                    connection.request(self.command, self.path, body=body, headers=dict(self.headers))
                    result = connection.getresponse()
                    payload = result.read()
                    if selected and 200 <= result.status < 300:
                        with owner.lock:
                            owner.successful_puts_before_failure += 1
                    self.send_response(result.status)
                    for key, value in result.getheaders():
                        if key.lower() not in ('connection', 'transfer-encoding', 'content-length'):
                            self.send_header(key, value)
                    # HEAD retains the upstream object's size, with no body.
                    length = result.getheader('Content-Length', '0') if self.command == 'HEAD' else str(len(payload))
                    self.send_header('Content-Length', length)
                    self.end_headers()
                    self.wfile.write(payload)
                finally:
                    connection.close()

            do_GET = do_HEAD = do_PUT = do_POST = do_DELETE = forward

        self.server = ThreadingHTTPServer(('127.0.0.1', 19002), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def start(self):
        self.thread.start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)
