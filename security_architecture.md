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
