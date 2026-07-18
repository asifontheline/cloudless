# cloudless — working title (M1a)

Group-private inference mesh serving the standard chat-completions API wire
format — see [../mvp_design.md](../mvp_design.md).
Current state: gateway with health probing, latency-ranked routing, automatic
failover, and gossip-based peer discovery (encrypted with a shared cluster secret).

Dependencies: `hashicorp/memberlist` (MPL-2.0) only. License: Apache-2.0
(add the canonical LICENSE text before publishing).

## Run

```sh
go build -o cloudless ./cmd/cloudless
cp config.example.json config.json   # edit backends to point at your local inference runtime /v1 endpoints
./cloudless serve -config config.json
```

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
