# Cloudless Open Protocol

The platform is defined by its HTTP API, not by any one language. Anything you can send an
HTTP request from can use and extend Cloudless. This document is the contract; a formal
OpenAPI spec is tracked in [#58](../../issues/58).

All management endpoints accept the cluster admin key or a member API key as
`Authorization: Bearer <key>`. Base URL is `http://<node>:8080`.

## Inference (standard chat-completions wire format)

```
POST /v1/chat/completions      # streaming or non-streaming; routed + failed over across the mesh
POST /v1/batch                 # parallel fan-out: many independent requests, divided across nodes
POST /v1/embeddings            # (planned)
GET  /v1/models
GET  /openapi.yaml               # this contract, as a formal machine-readable spec (served by every node)
```

Batch fan-out: `{"path": "/v1/chat/completions", "requests": [ {...}, ... ]}` (1–64 items)
returns `{"results": [ {"status", "backend", "body"}, ... ]}` in submission order. Items are
processed concurrently across healthy nodes; each item keeps single-request semantics —
failover before a complete response, backpressure, quota and usage metering.

Point any existing chat-completions client at any node — a base-URL swap is the whole
integration.

## Platform API (language-agnostic)

| Area | Endpoint | Purpose |
|---|---|---|
| Status | `GET /status` | Nodes, health, recent routes |
| Model commons | `GET /store`, `PUT /store?name=`, `POST /store/pull?name=`, `GET /store/verify?name=` | Hash-verified model store + mesh pull |
| Usage | `GET /usage` | Per-key/node requests and tokens |
| Ledger | `GET /ledger` | Contribution vs consumption |
| Cost | `GET /savings` | Hosted-API equivalent of mesh work |
| Capacity | `GET /capacity` | Idle-node surfacing |
| Members | `GET/POST /keys`, `DELETE /keys/{prefix}` | Per-user API keys (admin) |
| Sharing | `GET /share`, `PUT /share` | Resource share limits (5%–70%) |
| Membership | `POST /revoke/{name}`, `GET /revocations` | Node revocation (admin) |
| Map | `GET /status` + `location` field | Geo topology (continent/country/state/city/village) |

## Extending in your language

**As a client / SDK** — wrap these endpoints idiomatically. Example (Python):

```python
import requests
r = requests.post("http://localhost:8080/v1/chat/completions",
                  headers={"Authorization": "Bearer " + KEY},
                  json={"model": "your-model", "messages": [{"role": "user", "content": "hi"}]})
print(r.json()["choices"][0]["message"]["content"])
```

**As a new service / workload** — a node can dispatch work to an extension that speaks this
API over a subprocess, or run it as a sandboxed WASM module. No Go required; the extension
model is [#61](../../issues/61).

**As a runtime backend** — a backend (e.g. a Python worker serving a model) that exposes the
`/v1` wire format can sit behind a node's Runtime interface just like the built-in ones
([#62](../../issues/62)).

The contract is stable and versioned; breaking changes go through a new version, never a
silent change. Build freely.

_Contributions welcome in any language — see CONTRIBUTING.md._
