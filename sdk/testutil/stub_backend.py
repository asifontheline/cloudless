#!/usr/bin/env python3
"""Minimal chat-completions stub backend for SDK conformance tests (L4).

Stdlib only, mirrors the shape the real e2e Go harness uses: /models for
health probes, /chat/completions for inference. Serves a fixed, predictable
response so both SDKs can assert on exact content.
"""
import argparse
import json
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/models":
            self._json(200, {"data": []})
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        if self.path == "/chat/completions":
            self._json(200, {
                "choices": [{"message": {"role": "assistant", "content": "hello from the stub backend"}}],
                "usage": {"prompt_tokens": 3, "completion_tokens": 5},
            })
        else:
            self._json(404, {"error": "not found"})

    def _json(self, status, obj):
        payload = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, required=True)
    args = ap.parse_args()
    print(f"stub backend listening on 127.0.0.1:{args.port}", flush=True)
    HTTPServer(("127.0.0.1", args.port), Handler).serve_forever()
