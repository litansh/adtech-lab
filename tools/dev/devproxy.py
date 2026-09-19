# Dev-only origin: static Publisher + ad-server on one origin, as CloudFront will do.
import http.server, os, socketserver, urllib.request, urllib.error
ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "apps", "publisher")
UP   = "http://127.0.0.1:8138"
DYN  = ("/ad/", "/event/", "/collect")

class H(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **k): super().__init__(*a, directory=ROOT, **k)
    def _proxy(self, body=None):
        url = UP + self.path
        req = urllib.request.Request(url, data=body, method=self.command)
        for h in ("Content-Type",):
            if self.headers.get(h): req.add_header(h, self.headers[h])
        try:
            with urllib.request.urlopen(req) as r:
                self.send_response(r.status)
                for k, v in r.headers.items():
                    if k.lower() not in ("transfer-encoding","connection"): self.send_header(k, v)
                self.end_headers(); self.wfile.write(r.read())
        except urllib.error.HTTPError as e:
            self.send_response(e.code)
            for k, v in e.headers.items():
                if k.lower() in ("location","content-type"): self.send_header(k, v)
            self.end_headers()
            try: self.wfile.write(e.read())
            except Exception: pass
        except Exception as ex:
            self.send_error(502, str(ex))
    def do_GET(self):
        if self.path.startswith(DYN): return self._proxy()
        return super().do_GET()
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        return self._proxy(self.rfile.read(n))
    def log_message(self, *a): pass

socketserver.ThreadingTCPServer.allow_reuse_address = True
socketserver.ThreadingTCPServer(("127.0.0.1", 8139), H).serve_forever()
