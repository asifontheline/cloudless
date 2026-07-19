# Cloudless User Guide

A practical how-to for everything you can do with Cloudless today. If you can run one
command and open a browser, you can run a private mesh cloud.

## 1. Install & start your first node

```sh
# build from source (Go 1.22+)
git clone https://github.com/asifontheline/cloudless
cd cloudless/cloudless && go build -o cloudless ./cmd/cloudless

# start — detects a local model runtime, generates keys, prints an invite
./cloudless up
```

You'll see:

```
Console:  http://127.0.0.1:8080/          your private control panel (this machine only)
API:      http://<this-ip>:8080/v1/chat/completions (Bearer <api key>)
Add a node: cloudless up -join <secret>@<your-ip>:7946
```

`127.0.0.1:8080` is your admin console — open it in a browser. The **invite line** is what you
share to grow the mesh.

## 2. Add more machines (grow the mesh)

On a second machine (reachable over your LAN, VPN, or public IP), paste the invite:

```sh
./cloudless up -join <secret>@<first-node-ip>:7946
```

It enrolls (gets a certificate), joins the encrypted gossip mesh, and starts serving. Tag a
node's location so it shows on the map:

```sh
./cloudless up -location asia/india/karnataka/bengaluru
```

## 3. Use the AI (from anything)

Point any standard chat-completions client at any node:

```sh
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <api key>" \
  -d '{"model":"<your-model>","messages":[{"role":"user","content":"hi"}]}'
```

From Python (zero-dependency SDK):

```python
from cloudless import Client
mesh = Client("http://localhost:8080", api_key="<api key>")
print(mesh.chat("Explain the mesh in one line"))
```

Requests route to the best healthy node and fail over automatically if one drops.

**Failover semantics (exact):** the gateway retries the next-best node on any failure
that happens *before the first byte reaches you* — connection errors, 5xx responses,
streams that die before their first token, and buffered responses that fail mid-read.
You either get one clean, complete answer or one clean error; never a stitched-together
response. Once bytes have started flowing, a mid-stream node loss ends that stream
(re-issue the request — standard clients handle this as a normal retry); the `Retries`
column on the console's Recent Routes table shows how often failover saved a request.

## 4. The console — one place for everything

Open `http://127.0.0.1:8080/`. Tabs:

- **Dashboard** — live nodes, health, latencies, recent routes.
- **Map** — every node by locality (green = up, red = down), expandable continent → village.
- **Cloud AI / Storage / Compute / Data** — the service catalog.
- **Operations** — Usage & quotas, Contribution ledger, Cost calculator, Idle capacity.
- **Security** — Members & API keys, Resource sharing, Audit log, (passwordless sign-in, in build).
- **Features** — every capability with its status and issue link.

Admin actions in the console need the cluster key (from `~/.cloudless/config.json`) — paste it
once on the Members page; it's stored only in your browser.

## 5. Common tasks (CLI + console + API all work)

| Task | CLI | Console | API |
|---|---|---|---|
| See the mesh | `cloudless status` | Dashboard / Map | `GET /status` |
| Add a member key | `cloudless keys create alice` | Security → Members | `POST /keys` |
| Revoke a key | `cloudless keys revoke <prefix>` | Security → Members | `DELETE /keys/{prefix}` |
| Set how much you share | `cloudless share set -cpu 40 -when charging` | Security → Resource Sharing | `PUT /share` |
| Add a model | `cloudless models add model.gguf` | Storage → Model Commons | `PUT /store` |
| Pull a model from peers | `cloudless models pull model.gguf` | Storage → Model Commons | `POST /store/pull` |
| Verify a model | `cloudless models verify model.gguf` | Storage → Model Commons | `GET /store/verify` |
| See usage | `cloudless usage` | Operations → Usage | `GET /usage` |
| See who gave/used | `cloudless ledger` | Operations → Contribution Ledger | `GET /ledger` |
| Cost vs cloud | `cloudless savings` | Operations → Cost Calculator | `GET /savings` |
| Find idle nodes | `cloudless capacity` | Operations → Idle Capacity | `GET /capacity` |
| Evict a node | `cloudless nodes revoke <name>` | Map → revoke | `POST /revoke/{name}` |
| Check the audit log | `cloudless audit` | Security → Audit Log | `GET /audit` |

## 6. How much you share (safe by default)

Every node starts at a safe **5%** of CPU and can be tuned up to a **70%** ceiling — never
100%, so your machine stays responsive. On phones a thermal/battery guard pauses sharing if it
warms up or runs low (in build). Change it anytime:

```sh
cloudless share            # show current
cloudless share set -cpu 70 -when always   # a dedicated/idle box: turn it up
cloudless share set -cpu 0                 # stop sharing
```

Contribution is metered on the ledger, and you earn service in proportion to what you give.

## 7. Models: the commons

Add a model once, and peers can pull it from each other instead of re-downloading. Only safe
tensor formats are accepted (`.gguf`, `.safetensors`, `.onnx`) — pickle-based files are rejected
because they can execute code. Every artifact is SHA-256 verified on the way in and on demand.

## 8. Security you get for free

- Encrypted, authenticated gossip; cluster CA with mutual-TLS between nodes.
- Node revocation: evict a machine and its certificate is refused mesh-wide.
- Tamper-evident audit log of every admin action.
- Data stays on your hardware; nothing is sent to a vendor.

## 9. Contribute

You don't need to know Go. Build in any language against the open API (see
[PROTOCOL.md](https://github.com/asifontheline/cloudless/blob/main/PROTOCOL.md)); Python is
first-class. Changes flow through: branch → pull request → CI validation → review → merge queue.
Full guide: [CONTRIBUTING.md](https://github.com/asifontheline/cloudless/blob/main/CONTRIBUTING.md).

## 10. Troubleshooting

- **"no local runtime detected"** — start a local inference runtime first, or `-backend <url>`.
- **A peer won't join** — the first node must be reachable (LAN, VPN, public IP, or forwarded
  port 7946). A one-click overlay is in build.
- **Passkeys** — passwordless sign-in is in build; today the console uses the cluster key.
