"""Tiny stdlib-only mock API used by the example zone and the test suite.

    python examples/mock_api.py [port]

Endpoints: POST /auth/login, GET/POST /pets, GET/DELETE /pets/{id}, GET /health, GET /slow?ms=N
"""
from __future__ import annotations

import json
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

TOKEN = "demo-token-123"
LOCK = threading.Lock()
PETS = {1: {"id": 1, "name": "Rex", "kind": "dog", "age": 3}, 2: {"id": 2, "name": "Tom", "kind": "cat", "age": 5}}
NEXT_ID = [3]


class Handler(BaseHTTPRequestHandler):
    server_version = "MockPetstore/1.0"

    def log_message(self, *args):  # quiet
        pass

    def _send(self, status: int, payload=None, headers: dict | None = None) -> None:
        body = b"" if payload is None else json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.end_headers()
        if body:
            self.wfile.write(body)

    def _body(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        try:
            return json.loads(raw) if raw else {}
        except ValueError:
            return {"_raw": raw.decode(errors="replace")}

    def _authed(self) -> bool:
        return self.headers.get("Authorization") == f"Bearer {TOKEN}"

    def do_GET(self):
        url = urlparse(self.path)
        parts = url.path.strip("/").split("/")
        if parts == ["health"]:
            return self._send(200, {"status": "ok", "time": time.time()})
        if parts == ["slow"]:
            ms = int(parse_qs(url.query).get("ms", ["100"])[0])
            time.sleep(ms / 1000)
            return self._send(200, {"slept_ms": ms})
        if parts == ["echo"]:
            return self._send(200, {"headers": dict(self.headers), "query": parse_qs(url.query)})
        if not self._authed():
            return self._send(401, {"error": "missing or invalid bearer token"})
        if parts == ["pets"]:
            kind = parse_qs(url.query).get("kind", [None])[0]
            with LOCK:
                items = [p for p in PETS.values() if kind is None or p["kind"] == kind]
            return self._send(200, {"items": items, "total": len(items)}, {"X-Total-Count": str(len(items))})
        if len(parts) == 2 and parts[0] == "pets" and parts[1].isdigit():
            with LOCK:
                pet = PETS.get(int(parts[1]))
            return self._send(200, pet) if pet else self._send(404, {"error": "no such pet"})
        self._send(404, {"error": "not found"})

    def do_POST(self):
        parts = self.path.split("?")[0].strip("/").split("/")
        data = self._body()
        if parts == ["auth", "login"]:
            if data.get("username") == "admin" and data.get("password") == "hunter2":
                return self._send(200, {"access_token": TOKEN, "token_type": "bearer", "expires_in": 3600})
            return self._send(401, {"error": "bad credentials"})
        if not self._authed():
            return self._send(401, {"error": "missing or invalid bearer token"})
        if parts == ["pets"]:
            if not data.get("name"):
                return self._send(400, {"error": "name is required"})
            with LOCK:
                pid = NEXT_ID[0]
                NEXT_ID[0] += 1
                pet = {"id": pid, "name": data["name"], "kind": data.get("kind", "unknown"), "age": data.get("age", 0)}
                PETS[pid] = pet
            return self._send(201, pet, {"Location": f"/pets/{pid}"})
        self._send(404, {"error": "not found"})

    def do_DELETE(self):
        parts = self.path.strip("/").split("/")
        if not self._authed():
            return self._send(401, {"error": "missing or invalid bearer token"})
        if len(parts) == 2 and parts[0] == "pets" and parts[1].isdigit():
            with LOCK:
                existed = PETS.pop(int(parts[1]), None) is not None
            return self._send(204) if existed else self._send(404, {"error": "no such pet"})
        self._send(404, {"error": "not found"})


def serve(port: int = 8765, background: bool = False):
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    server.daemon_threads = True
    if background:
        threading.Thread(target=server.serve_forever, daemon=True).start()
        return server
    print(f"mock petstore listening on http://127.0.0.1:{port}  (Ctrl+C to stop)")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    return server


if __name__ == "__main__":
    serve(int(sys.argv[1]) if len(sys.argv) > 1 else 8765)
