#!/usr/bin/env bash
# Orchestrates SDK conformance tests (L4, #87): builds the real cloudless
# binary, starts it against a stub backend, runs both SDKs' conformance
# suites against the live node, and tears everything down. Exits non-zero
# if the binary, the node, or either SDK suite fails.
set -euo pipefail
cd "$(dirname "$0")/../.."   # repo root
ROOT="$(pwd)"
WORK="$(mktemp -d)"
trap 'kill $STUB_PID $NODE_PID 2>/dev/null || true; rm -rf "$WORK"' EXIT

free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }

STUB_PORT=$(free_port)
API_PORT=$(free_port)
API_KEY="conformance-test-key"

echo "building cloudless..."
(cd cloudless && go build -o "$WORK/cloudless" ./cmd/cloudless)

python3 "$ROOT/sdk/testutil/stub_backend.py" --port "$STUB_PORT" &
STUB_PID=$!

cat > "$WORK/config.json" <<EOF
{
  "listen": "127.0.0.1:$API_PORT",
  "api_key": "$API_KEY",
  "health_interval_seconds": 1,
  "backends": [{"name": "stub", "base_url": "http://127.0.0.1:$STUB_PORT"}]
}
EOF

(cd "$WORK" && "$WORK/cloudless" serve -config config.json) &
NODE_PID=$!

echo -n "waiting for node..."
for i in $(seq 1 100); do
  if curl -sf "http://127.0.0.1:$API_PORT/healthz" >/dev/null 2>&1; then echo " up"; break; fi
  sleep 0.1
  if [ "$i" -eq 100 ]; then echo " TIMEOUT"; exit 1; fi
done

export CLOUDLESS_TEST_ADDR="http://127.0.0.1:$API_PORT"
export CLOUDLESS_TEST_KEY="$API_KEY"

echo "=== Python SDK conformance ==="
PYTHONPATH="$ROOT/sdk/python" python3 -m unittest discover -s "$ROOT/sdk/python/tests" -v

echo "=== JS SDK conformance ==="
(cd "$ROOT/sdk/js" && node --test)

echo "all SDK conformance suites passed"
