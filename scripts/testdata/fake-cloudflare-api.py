"""A stand-in for the Cloudflare DNS API, enough to exercise the script.

Records live in memory. Every response has the success/result/errors envelope
the real API uses, because the script's error handling depends on it.
"""

import json
import re
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

ZONE_ID = "zone123"
ZONE_NAME = "example.com"

records = {}
next_id = [0]
lock = threading.Lock()


def envelope(result, success=True, errors=None):
    return json.dumps({
        "success": success,
        "errors": errors or [],
        "messages": [],
        "result": result,
    }).encode()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _auth_ok(self):
        return self.headers.get("Authorization") == "Bearer test-token"

    def _send(self, body, code=200):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _body(self):
        length = int(self.headers.get("Content-Length", 0))
        return json.loads(self.rfile.read(length)) if length else {}

    def do_GET(self):
        if not self._auth_ok():
            return self._send(envelope(None, False, [{"code": 10000, "message": "Authentication error"}]), 403)
        url = urlparse(self.path)
        q = parse_qs(url.query)

        m = re.match(r"^/client/v4/zones/([^/]+)/email/routing$", url.path)
        if m:
            import os
            mode = os.environ.get("ROUTING", "off")
            if mode == "forbidden":
                return self._send(envelope(None, False, [{"code": 10000, "message": "Authentication error"}]), 403)
            return self._send(envelope({"enabled": mode == "on", "status": mode}))

        if url.path == "/client/v4/zones":
            name = q.get("name", [""])[0]
            result = [{"id": ZONE_ID, "name": ZONE_NAME}] if name == ZONE_NAME else []
            return self._send(envelope(result))

        m = re.match(r"^/client/v4/zones/([^/]+)/dns_records$", url.path)
        if m:
            rtype = q.get("type", [""])[0]
            name = q.get("name", [""])[0]
            with lock:
                found = [r for r in records.values() if r["type"] == rtype and r["name"] == name]
            return self._send(envelope(found))

        self._send(envelope(None, False, [{"code": 7003, "message": "Could not route"}]), 404)

    def do_POST(self):
        if not self._auth_ok():
            return self._send(envelope(None, False, [{"code": 10000, "message": "Authentication error"}]), 403)
        m = re.match(r"^/client/v4/zones/([^/]+)/dns_records$", urlparse(self.path).path)
        if not m:
            return self._send(envelope(None, False, [{"code": 7003, "message": "Could not route"}]), 404)
        body = self._body()
        with lock:
            next_id[0] += 1
            rid = f"rec{next_id[0]}"
            body["id"] = rid
            records[rid] = body
        self._send(envelope(body))

    def do_PATCH(self):
        if not self._auth_ok():
            return self._send(envelope(None, False, [{"code": 10000, "message": "Authentication error"}]), 403)
        m = re.match(r"^/client/v4/zones/([^/]+)/dns_records/([^/]+)$", urlparse(self.path).path)
        if not m:
            return self._send(envelope(None, False, [{"code": 7003, "message": "Could not route"}]), 404)
        rid = m.group(2)
        body = self._body()
        with lock:
            if rid not in records:
                return self._send(envelope(None, False, [{"code": 81044, "message": "Record not found"}]))
            body["id"] = rid
            records[rid] = body
        self._send(envelope(body))


if __name__ == "__main__":
    import sys

    # Seed a record that is wrong in the way that matters: proxied mail.
    # A realistic apex: a domain-verification TXT sitting before the SPF one,
    # which is what every zone that has ever been verified by anything looks like.
    if "--seed-apex-txt" in sys.argv:
        records["verif"] = {
            "id": "verif", "type": "TXT", "name": "example.com",
            "content": "google-site-verification=abc123", "proxied": False, "ttl": 1,
        }
        if "--no-spf" not in sys.argv:
            records["spf"] = {
                "id": "spf", "type": "TXT", "name": "example.com",
                "content": ("\"v=spf1 -all\"" if "--quoted-txt" in sys.argv else "v=spf1 -all"), "proxied": False, "ttl": 1,
            }

    if "--seed-proxied-mx-host" in sys.argv:
        records["seeded"] = {
            "id": "seeded", "type": "A", "name": "mx1.example.com",
            "content": "203.0.113.10", "proxied": True, "ttl": 1,
        }

    server = HTTPServer(("127.0.0.1", 8787), Handler)
    print("listening", flush=True)
    server.serve_forever()
