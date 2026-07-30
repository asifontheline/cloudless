# Security Architecture — The Protection Backbone

Peer-to-peer resource sharing is a hostile-takeover target: a compromised node can steal compute, exfiltrate data, or poison results. This document defines the layered protection backbone. One honesty rule up front: **"foolproof" does not exist in security — the design goal is defense in depth**, where every layer assumes the previous one failed, and the blast radius of any single compromise stays small.

## Threat model
| # | Threat | Vector |
|---|---|---|
| T1 | Mesh takeover | Stolen join secret; malicious node joins and receives traffic |
| T2 | Node compromise | A member's machine is hacked; attacker now sits *inside* the trust boundary |
| T3 | Malicious workload | A task tries to escape its sandbox and take over host resources |
| T4 | Poisoned artifacts | Malware hidden in model files or containers pulled into the mesh |
| T5 | Eavesdropping / MITM | Traffic interception on untrusted networks |
| T6 | Resource abuse | A member (or stolen key) monopolizes group capacity |
| T7 | Supply chain | Malicious code entering via our own dependencies or build |
| T8 | Result poisoning | A compromised node returns wrong/backdoored outputs |

## Layer 1 — Membership & identity (T1, T2)
- Encrypted, authenticated gossip (AES-GCM cluster key) — **built**; wrong-key nodes cannot join.
- M1b: per-node certificates from a cluster CA; join tokens are **single-use and expiring**, minted from the console; mutual TLS on every connection.
- **Revocation as a first-class action:** one console click evicts a node — cert revoked, gossip key rotated, peers reconfigure automatically. Assume any member machine can be stolen.
- Key rotation on schedule, not only on incident.

## Layer 2 — Transport (T5)
- Mutual TLS between all nodes (M1b); optional encrypted overlay beneath it (M5) = two encryption layers on hostile networks.
- No plaintext listener anywhere; the gateway's public port carries only the service API with Bearer auth.

## Layer 3 — Workload isolation (T3)
- **v0.1's strongest defense is scope:** nodes execute *inference only* — no arbitrary code from peers, ever. The attack surface is a JSON API in front of a supervised runtime.
- When general compute arrives (containers/functions milestones), workloads run in rootless containers with: no host filesystem access, dropped capabilities, seccomp profiles, memory/CPU cgroup caps, and no outbound network by default (egress must be declared).
- The agent itself runs as an unprivileged user; a compromised runtime process cannot reconfigure the node.

**Extensions (K4/K5, S4) — a narrower exception, worth being precise about.** An
admin can register any HTTP service, in any language, at `/x/<name>/...` and,
since K5, as a real inference backend. This is *not* arbitrary code execution by
the gateway: the extension is a process the operator already started and pointed
the gateway at — the gateway only proxies to it, never spawns, never executes it.
The trust boundary is exactly the operator's own admin key, same as any other
admin-gated action.
- **What's already true:** the gateway strips mesh credentials before forwarding
  to an extension (it never sees the cluster's bearer tokens); registration is
  admin-only and audited (`ext.register`/`ext.remove` in the signed audit log);
  the extension itself runs under whatever OS user and permissions the operator
  chose — the gateway has no ability to grant or escalate those.
- **What the gateway can't enforce:** network egress from the extension's own
  process. It isn't spawned by the gateway, so there's no process tree to apply
  cgroup/seccomp/no-egress-by-default policy to (unlike the future containerized
  compute above, which the mesh *does* control the lifecycle of).
- **Operator guidance, not gateway enforcement:** run extensions in a container,
  VM, or restricted OS account with only the network access the extension
  actually needs — typically none outbound beyond what it must call. Don't run
  an extension as the same user as the `cloudless` process itself. This is the
  honest boundary: the mesh secures everything on its side of the proxy; what
  runs on the other side is the operator's own deployment choice.

## Layer 4 — Artifact integrity & malware defense (T4)
- **Content addressing everywhere:** every model blob and container image is identified by SHA-256; a byte that changes is a different artifact. Peers verify hashes before serving *and* before loading — a poisoned cache replica is detected, not executed.
- **Safe model formats only:** weights are accepted exclusively in tensor-data formats (GGUF, safetensors, ONNX). Pickle-based model files are **rejected outright** — they can embed arbitrary code and are the main malware vector in the model ecosystem.
- Signed artifact manifests: the member who introduces an artifact signs it; provenance is visible in the console.
- Optional scan hook on the blob store (open-source scanners) for general file artifacts; note honestly: scanners catch known signatures, not novel implants — the hash + format + provenance layers are the real defense.

## Layer 5 — Detection & response (T2, T6, T8)
- **Signed append-only audit log** of every administrative action and artifact introduction (blueprint §4) — tamper-evident history.
- Behavioral monitoring per node, surfaced in the console: unexpected egress attempts, failed-auth spikes, latency/output anomalies, resource use outside advertised capacity. Anomalies quarantine a node (traffic drained, membership suspended) pending review.
- Reputation: nodes accrue trust from verified good behavior; routing prefers trusted nodes (rule engine already does this by health — extend with integrity signals).
- For critical jobs: **k-of-n redundant execution with result comparison** (rule engine) — a lying node is outvoted and flagged. This is the practical answer to result poisoning without exotic cryptography.
- Per-key quotas and rate limits stop resource monopolization; usage accounting (M3) makes abuse visible.

## Layer 6 — Encryption everywhere & Data Guard (T2, T4, T5)
Encryption is universal — mesh control traffic, service traffic, and every byte at rest — and Data Guard governs where data may *go*, not just who may read it. (Per the honesty rule: this is layered defense with stated limits, not "foolproof" — a fully compromised node can read what that node legitimately processes in memory.)

**Encryption in transit (three layers):**
- Gossip/membership: AES-GCM with rotating cluster key — built.
- Service + peer traffic: mutual TLS from the cluster CA on every connection; zero plaintext listeners (M1b).
- Optional encrypted overlay beneath everything for hostile networks (M5).

**Encryption at rest (every node store):**
- All node-held data — model blobs, cached artifacts, configs, accounting DB, audit log — encrypted with per-node data keys (AES-256-GCM), wrapped by the cluster key hierarchy.
- Key hierarchy: root (offline/console-held) → cluster keys → per-node keys → per-artifact keys; scheduled rotation; compromise of one tier never exposes the tier above.
- Node keys unlocked at agent start via OS keystore or passphrase; never stored plaintext on disk.

**Data Guard (where data may go):**
- **Classification:** every artifact and dataset is labeled `private` (never leaves its origin node), `group` (replicates only inside the mesh), or `public`. Default is `private` — sharing is opt-in, matching the blueprint's privacy-first principle.
- **Locality enforcement:** the store and scheduler refuse to replicate or route `private` data off-node; `group` data never exits the mesh boundary.
- **Egress guard:** workloads get no outbound network by default; any egress must be declared and is logged.
- **Movement audit + leak detection:** every data transfer is recorded in the signed audit log; anomalous volume/destination patterns quarantine the node (Layer 5).
- **Crypto-shredding:** deletion destroys the artifact's key — the ciphertext everywhere becomes unreadable, including on peers and backups.
- **Recovery without a vendor:** cluster root key recoverable via k-of-n secret shares held by group members — no single member, and no outsider, can unlock alone.

## Layer 7 — Supply chain (T7)
- Minimal dependencies (currently one library beyond the standard library), pinned and checksum-locked; dependency review on every addition per the licensing policy.
- Reproducible builds and signed release binaries with published checksums.
- The web console is embedded with zero external assets — no CDN scripts, no third-party trackers, nothing fetched at runtime. This is already policy and also a security property.

## Honest limits (write these in the paper)
1. A fully compromised *member machine* with valid credentials can misuse whatever that member could access until detected — layers 5's job is shrinking that window, not eliminating it.
2. Malware scanning is signature-based; the stronger guarantees come from hash verification, safe formats, sandboxing, and least privilege.
3. Open (stranger-to-stranger) federation is deferred *because* these guarantees only hold in the trusted-group model; it must not ship before remote attestation / reputation / validation layers exist.

## Build order
- **Now (M1b):** cluster CA, single-use join tokens, mTLS, console-driven revocation.
- **M2:** hash verification on every blob path; format allowlist (reject pickle).
- **M3:** per-key quotas, signed audit log.
- **M4+:** anomaly quarantine, k-of-n result comparison, signed releases.

## Incident response runbook (S3)
Concrete steps using the actual shipped tooling — not the design intent above, what
today's `cloudless` binary and gateway API can actually do right now. Written for
whoever is holding the cluster admin key when something goes wrong.

### A node is compromised
1. **Evict it immediately:** `cloudless nodes revoke <node-name>` (or the console's
   Members page). This drops its certificate and removes it from routing mesh-wide —
   every peer refuses it on the next gossip round (A4).
2. **Check what it did while trusted:** `cloudless audit` (or `GET /audit`) — the
   hash-chained log shows every administrative action; `intact: true` confirms it
   hasn't been tampered with. If `GET /healthz` is returning 503 (S5), the chain
   itself may be compromised — treat the whole node's history as unverifiable.
3. **Rotate anything the node could have read:** any vault objects (M3) it held
   replicas of are still safe (ciphertext only, sealed by the owner's key) — but if
   the *owner* node is the one compromised, rotate that vault's contents once you've
   rebuilt the node.
4. **Check its usage pattern:** `cloudless usage` / `cloudless ledger` for anomalous
   token consumption in the window before you noticed — informs whether this was
   automated abuse or a one-off.

### A member's API key is leaked
1. `cloudless keys revoke <key-prefix>` — immediate, no mesh-wide propagation delay
   since key checks happen at the gateway serving the request.
2. Issue a replacement: `cloudless keys create <name>`.
3. Check `cloudless usage` for that key's consumption right up to revocation — a
   sudden spike is the signal a leak was actually exploited, not just theoretical.

### A malicious extension was registered
1. `cloudless ext rm <name>` — stops both `/x/<name>/...` proxying and, if it had
   `inference: true` (K5), pulls it out of `/v1/chat/completions` routing immediately.
2. Registration is admin-only and audited (`ext.register` / `ext.remove` in
   `cloudless audit`) — confirm who registered it and when.
3. Extensions are proxied HTTP services under their own OS permissions, never
   executed by the gateway (S4) — the blast radius is whatever that process itself
   could reach, not the node's Go runtime.

### The cluster secret itself leaks (not just one node's credentials)
**Honest gap:** there is no in-place rotation for the shared gossip/join secret
today — it's generated once at founding and used for both gossip encryption and
join-token HMAC. If the secret itself (not a single node's cert) is exposed:
1. Treat every node's continued membership as suspect — an attacker with the raw
   secret can join directly, bypassing per-node revocation entirely.
2. The only real mitigation right now is a fresh start: stand up a new mesh (new
   `cloudless up`, new secret), export data from the old one (`cloudless backup
   export`) and import it into the new (`cloudless backup import`, M5) — the
   archive travels encrypted regardless of which mesh it lands in.
3. In-place cluster-secret rotation without a full rebuild is a real gap, not yet
   tracked as its own story — worth adding to Epic A or S if it recurs as a need.

### General principle
Revoke first, investigate after — `cloudless nodes revoke` / `cloudless keys
revoke` / `cloudless ext rm` all take effect immediately and are cheap to reverse
(re-issue) if you were wrong. The signed audit log means investigation evidence
doesn't depend on acting slowly.
