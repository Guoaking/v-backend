#!/usr/bin/env python3
import json
import time
import random
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

HOST = "127.0.0.1"
PORT = 8082

class Handler(BaseHTTPRequestHandler):
    def _set_headers(self, code=200):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization")
        self.end_headers()

    def do_OPTIONS(self):
        self._set_headers(204)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length > 0 else b"{}"
        try:
            data = json.loads(body.decode("utf-8"))
        except Exception:
            data = {}

        path = urlparse(self.path).path
        
        # /api/v1/kyc/liveness/action/session
        if path.endswith("/kyc/liveness/action/session"):
            resp = {
                "success": True,
                "message": "Session created",
                "data": {
                    "session_id": f"mock-session-{int(time.time())}",
                    "upload_id": f"mock-upload-{int(time.time())}",
                    "trace_id": f"mock-trace-{int(time.time())}",
                    "actions": ["blink", "nod", "mouth_open"],
                    "expires_at": "2099-12-31T23:59:59Z"
                }
            }
            self._set_headers(200)
            self.wfile.write(json.dumps(resp).encode("utf-8"))
            
        # /api/v1/kyc/liveness/action/upload
        elif path.endswith("/kyc/liveness/action/upload"):
            resp = {
                "success": True,
                "message": "Upload successful",
                "data": {
                    "upload_id": "mock-upload-id"
                }
            }
            self._set_headers(200)
            self.wfile.write(json.dumps(resp).encode("utf-8"))
            
        # /api/v1/keys (Mock keys for token generation)
        elif path.endswith("/keys"):
            resp = {
                "success": True,
                "data": [
                    {"id": "mock-key-1", "status": "active", "name": "Default Key"}
                ]
            }
            self._set_headers(200)
            self.wfile.write(json.dumps(resp).encode("utf-8"))
            
        # /api/v1/keys/reveal (Mock token reveal)
        elif "/keys/" in path and "/reveal" in path:
            resp = {
                "success": True,
                "data": {
                    "secret": "mock-secret-token-123"
                }
            }
            self._set_headers(200)
            self.wfile.write(json.dumps(resp).encode("utf-8"))

        else:
            self._set_headers(404)
            self.wfile.write(json.dumps({"success": False, "message": "Not found"}).encode("utf-8"))

    def do_GET(self):
        parsed_path = urlparse(self.path)
        path = parsed_path.path
        query = parse_qs(parsed_path.query)

        # /api/v1/kyc/liveness/action/result
        if path.endswith("/kyc/liveness/action/result"):
            # Randomly decide pass or fail, or based on session_id if needed
            # For stability in tests, let's make it pass mostly
            resp = {
                "success": True,
                "data": {
                    "passed": True,
                    "score": 0.95,
                    "action_consistency": {
                        "blink": "pass",
                        "nod": "pass",
                        "mouth_open": "pass"
                    },
                    "message": "Verification passed"
                }
            }
            self._set_headers(200)
            self.wfile.write(json.dumps(resp).encode("utf-8"))
            
        # /api/v1/keys (Mock keys list)
        elif path.endswith("/keys"):
             resp = {
                "success": True,
                "data": [
                    {"id": "mock-key-1", "status": "active", "name": "Default Key"}
                ]
            }
             self._set_headers(200)
             self.wfile.write(json.dumps(resp).encode("utf-8"))

        # /api/v1/auth/me (Mock user for ProtectedRoute)
        elif path.endswith("/auth/me"):
             resp = {
                "success": True,
                "data": {
                    "id": "mock-user-1",
                    "email": "mock@example.com",
                    "name": "Mock User",
                    "role": "user",
                    "organizations": [{"id": "org-1", "name": "Test Org"}]
                }
            }
             self._set_headers(200)
             self.wfile.write(json.dumps(resp).encode("utf-8"))

        else:
            self._set_headers(404)
            self.wfile.write(json.dumps({"success": False, "message": "Not found"}).encode("utf-8"))

def run():
    server = HTTPServer((HOST, PORT), Handler)
    print(f"Mock Liveness Action server running at http://{HOST}:{PORT}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    server.server_close()

if __name__ == "__main__":
    run()
