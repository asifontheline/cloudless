#!/usr/bin/env python3
"""A Cloudless extension in ~60 lines of stdlib Python (K4).

Run an ordinary HTTP service, register it with your node, and it's live for
every member at /x/summarize/... — same bearer keys as inference, and your
mesh credentials never reach this process (the gateway strips them).

    python3 summarize.py --port 9090 \
        --node http://127.0.0.1:8080 --admin-key <cluster key>

Then, from anywhere on the mesh:

    curl http://<node>:8080/x/summarize/run \
      -H "Authorization: Bearer <member key>" \
      -d '{"text": "a long passage ..."}'

Any language works the same way: serve HTTP, POST /extensions once.
"""
import argparse
import json
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        # /healthz answers the node's health probe.
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        try:
            body = json.loads(self.rfile.read(length) or b"{}")
            text = str(body.get("text", ""))
        except json.JSONDecodeError:
            self.send_response(400)
            self.end_headers()
            return
        # A deliberately simple "summary": first sentence + word count.
        # Swap this for a call back to the mesh's own /v1/chat/completions
        # to build AI-powered extensions out of the mesh itself.
        first = text.split(".")[0].strip()
        summary = {"summary": first, "words": len(text.split())}
        payload = json.dumps(summary).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *args):
        pass


def register(node, admin_key, port):
    req = urllib.request.Request(
        node + "/extensions",
        data=json.dumps({
            "name": "summarize",
            "base_url": f"http://127.0.0.1:{port}",
            "runtime": "python",
            "description": "First-sentence summary + word count (example extension)",
        }).encode(),
        headers={"Authorization": "Bearer " + admin_key,
                 "Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req) as resp:
            print("registered with node:", resp.read().decode())
    except urllib.error.HTTPError as e:
        detail = e.read().decode()
        if "already registered" in detail:
            print("already registered — serving")
        else:
            raise SystemExit(f"registration failed: {detail}")


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=9090)
    ap.add_argument("--node", default="http://127.0.0.1:8080")
    ap.add_argument("--admin-key", required=True)
    args = ap.parse_args()
    register(args.node, args.admin_key, args.port)
    print(f"serving on 127.0.0.1:{args.port} — reachable at {args.node}/x/summarize/…")
    HTTPServer(("127.0.0.1", args.port), Handler).serve_forever()
