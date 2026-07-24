# cloudless — working title (M1a)

Group-private inference mesh serving the standard chat-completions API wire
format — see [../mvp_design.md](../mvp_design.md).
Current state: gateway with health probing, latency-ranked routing, automatic
failover, and gossip-based peer discovery (encrypted with a shared cluster secret).

Dependencies: `hashicorp/memberlist` (MPL-2.0), `skip2/go-qrcode` (MIT) — full
list with upstream licenses in [../NOTICE](../NOTICE). License: Apache-2.0,
see [../LICENSE](../LICENSE).

## Run

```sh
go build -o cloudless ./cmd/cloudless
cp config.example.json config.json   # edit backends to point at your local inference runtime /v1 endpoints
./cloudless serve -config config.json
```

## Stop

`Ctrl+C` (SIGINT) or `SIGTERM` triggers a graceful shutdown: the HTTP server
stops accepting new requests, in-flight ones get up to 5s to finish, and (in
gossip mode) the node leaves the mesh cleanly so peers drop it from routing
right away instead of waiting for a health-check timeout.

**If `Ctrl+C` does nothing:** you're almost certainly pressing it in a
terminal that isn't the process's controlling terminal — most commonly
because the process was started backgrounded with `&` (as in `./cloudless
serve -config config.json &`), which detaches it from the shell's signal
handling. `Ctrl+C` only reaches the terminal's *foreground* process group.

To stop it from another terminal:

```sh
# find it
ps aux | grep '[c]loudless'
# stop it gracefully (same signal Ctrl+C sends)
kill -INT <pid>
# or, if you only know the port it's listening on
lsof -ti:8080 -sTCP:LISTEN | xargs kill -INT
```

If it was started with `&` in a shell you still have open, `fg` first (bring
it to the foreground), then `Ctrl+C` works normally.

Point any standard chat-completions client at it:

```sh
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer <api_key from config>" \
  -d '{"model":"<your-model-id>","messages":[{"role":"user","content":"hi"}]}'
```

Inspect the mesh:

```sh
./cloudless status -addr http://127.0.0.1:8080
```

Measure real latency and throughput against it (D2) — a number you can
trust because it came from your own mesh, not a marketing claim:

```sh
./cloudless bench -addr http://127.0.0.1:8080 -key <api_key> -n 50 -c 8
```

Scrape node health, routing, and usage into your own monitoring (D3) — any
standard scraper can pull this, no proprietary agent required:

```sh
curl http://127.0.0.1:8080/metrics
```

## Mesh mode (gossip discovery)

Instead of a static `backends` list, give each node a `gossip` section — peers
then discover each other and the routing table follows the live mesh:

```json
{"listen":":8080","api_key":"cluster-key",
 "gossip":{"node_name":"node-b","bind":"0.0.0.0:7946",
           "join":["192.168.1.10:7946"],
           "backend_url":"http://192.168.1.42:11434/v1",
           "secret":"16-24-or-32-byte-shared-key"}}
```

The first node omits `join`. The `secret` encrypts and authenticates gossip
(AES-GCM) — nodes without it cannot join the mesh.

## Behavior

- Probes each backend's `GET /models` every `health_interval_seconds`.
- Routes to the fastest healthy backend; on connection failure or 5xx it
  retries the next-ranked backend (before any byte reaches the client).
- Streaming responses are flushed through as they arrive.
- `/status` returns backend health plus the last 20 routing decisions.

## Next (M1)

Gossip registry (memberlist), join tokens, mTLS between nodes — see the
design doc for the full milestone plan and licensing policy.
