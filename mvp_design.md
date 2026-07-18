# MVP Design — The Group-Private Inference Mesh

**Status:** Iteration 5. Body text uses only our own vocabulary and generic engineering terms; third-party software is referenced solely in Appendix A (open-source dependencies), and no external brand is part of our naming, features, or identity.

## The one-sentence idea
A tiny node agent you run on any spare machine that joins a private mesh of trusted peers; together they serve a single **standard chat-completions API endpoint** — with automatic routing, failover, and model caching — so a small group (team, lab, co-op) gets cloud-style AI service out of hardware they already own.

## Why this wedge (and not the whole blueprint)
The blueprint spans compute + storage + training + governance + open federation. That is five products. The strongest single wedge is **distributed inference for small trusted groups** because:

1. **Immediate, felt pain:** usage-based AI API bills and privacy concerns are the #1 reason people want to leave hosted providers today.
2. **Drop-in adoption:** we implement the de-facto industry wire format for chat-completion APIs, so existing tools and SDKs work on day one with a base-URL swap. Zero client-side integration cost. (This is wire-format compatibility only — no provider's name or branding is part of our product.)
3. **Trusted-group scoping deletes the hardest problems:** no byzantine trust, no credit economy, no result validation, no sandboxing of hostile code, no open-network discovery — invite-based mesh among people who already trust each other. Those systems become *later phases*, not prerequisites.
4. **It exercises the core thesis anyway:** heterogeneous hardware, scheduling, replication/failover, model distribution, encrypted peer-to-peer transport — the paper's rule engine gets a real testbed.

## Market gap (described generically)
Existing approaches cluster into four camps, none of which serves our niche:
- **Single-machine model runners** — polished local inference, but no pooling, no failover, no group service.
- **Model-sharding clusters** — split one large model across one owner's devices; not a multi-user, multi-model, failover-oriented *service* for a group.
- **Public volunteer swarms** — research-grade systems for sharing capacity with strangers; wrong trust model for private groups.
- **Token-incentivized compute marketplaces** — heavyweight blockchain economics aimed at monetizing capacity, not cooperative sharing.

**Our gap:** *simple, group-private, standards-compatible inference with failover, owned by the group itself.* Cooperative ownership and privacy-first processing (core principles) are the differentiators, not an afterthought.

## Product principle: web-managed, zero-friction setup (hard constraint)
**Everything must be manageable from a website, and setup time is a first-class cost to minimize.** Concretely:
- The node binary embeds a web console (no separate install, no external assets): mesh status, node/model management, join-token generation, usage view.
- Onboarding a node must be one command (`up`) or less — auto-detect an existing local runtime, generate defaults, print a join link/QR for the next machine. No hand-edited config files on the happy path.
- Every management action available in the console maps to the same HTTP API the CLI uses — web UI, CLI, and automation stay one surface.
- Any feature that requires multi-step manual setup is considered incomplete; the setup flow is part of the feature.

## Licensing and naming policy (hard constraint)
- **Every runtime dependency, bundled component, and default model must be under an OSI-approved license** (Apache-2.0, MIT, BSD, MPL-2.0, GPL where linkage allows) or public domain. No source-available/BUSL software, no proprietary SaaS control planes, no restricted-use model licenses. Restricted-license models are never bundled, redistributed, or set as defaults (users may load what they are licensed to use).
- **No third-party or cloud-provider name appears in our product naming, feature naming, UI, or marketing.** We may mimic *capabilities* of commercial clouds, but every feature is named in our own vocabulary and justified by our core principles (privacy-first, cooperative ownership, low-cost community hardware) — never as "X, like <brand>".
- Factual references to open-source dependencies live only in Appendix A (ordinary nominative use in engineering docs).
- Project's own license: **Apache-2.0** (patent grant, cooperative-friendly, matches the open-interoperability principle).

## Decisions (resolved)
1. **Runtime: supervise, don't embed.** The agent detects an existing local inference runtime or downloads a pinned open-source inference server binary per platform. Abstraction: a `Runtime` interface with `Load(model)`, `Infer(req) stream`, `Unload(model)` — one implementation per supported runtime.
2. **Networking: bring-your-own-network for v0.1.** Peers must be mutually reachable (LAN, or a self-hosted open-source VPN overlay). Mutual TLS runs on top, so an open LAN isn't a trust boundary. A bundled overlay is M5. This cuts weeks of NAT-traversal work from the critical path.
3. **Naming:** internal working title only for now; final name must be our own mark with no collision (see naming research note, kept separately). Domain shortlist verified available as of 2026-07-18.
4. **Usage accounting: yes, minimal, in M3.** Per-API-key token counts in a local embedded database on the gateway node — the seed of the eventual cooperative credit system without designing an economy now.
5. **Paper alignment:** the MVP directly serves the paper's §8 experiments 1–4; experiment 5 falls out of M4 telemetry. Paper claims about training, open federation, and credits should be marked "future work."

## v0.1 concrete spec

### CLI surface
```
<agent> init                  # mint cluster CA + first join token
<agent> join <token@host>     # fetch CA-signed cert, join gossip ring
<agent> serve                 # run agent (gateway + runtime + registry)
<agent> models pull <name>    # fetch via mesh cache, fall back to public model repositories
<agent> status                # nodes, models, queue depths, last 20 routes
```

### Gossip node state (<512 bytes)
```json
{"id":"n_ab12","addr":"10.0.0.7:9443","ver":"0.1.0",
 "models":[{"id":"open-7b-q4","ctx":8192,"tps_est":42}],
 "vram_free_mb":9200,"queue":1,"healthy":true,"ts":1721286000}
```

### Request path
1. Client hits any node: `POST /v1/chat/completions` (Bearer = cluster API key).
2. Gateway filters registry: peers with `model` loaded, `healthy`, seen <15s ago.
3. Score = `tps_est / (queue + 1)`; pick max; self counts as a peer.
4. Proxy with streaming; on connect failure or mid-stream drop **before first token**, retry next-best peer; after first token, surface the error (client retries) — resumable streams deferred.
5. Log route decision to a ring buffer exposed by `status` and the web console.

### Failure semantics (the rule engine, v0.1)
- Heartbeat via gossip; node marked suspect after 5s, dead after 15s.
- Gateway-side circuit breaker: 3 consecutive failures → peer excluded 60s.
- Long generations: no checkpointing in v0.1; cap `max_tokens` per cluster policy instead.

## Milestones
1. **M0 — two-node failover core.** Static peer list, gateway proxies to whichever node is alive.
2. **M1 — mesh formation.** (a) gossip registry + encrypted membership; (b) join tokens backed by a cluster CA + mutual TLS.
3. **M2 — model commons.** Pull-through content-addressed model cache; peers serve model blobs to each other.
4. **M3 — accounts.** Mid-stream failover retry, request queueing, per-user API keys, minimal usage accounting.
5. **M4 — evaluation.** 3–5 node deployment; measure latency/throughput/availability under node churn vs. single-node and hosted-API baselines (paper §8).
6. **M5 — bundled overlay networking + one-command onboarding (`up`).**

## Status log
- **M0 BUILT ✅ (2026-07-18).** Implemented in `cloudless/` (working title). Smoke-tested with two mock backends: latency-ranked routing, automatic failover when a backend is killed, Bearer-key auth, `status` output. Stdlib-only at this stage.
- **M1a BUILT ✅ (2026-07-18).** Gossip peer discovery; nodes advertise their inference endpoint; joins/leaves update the routing table live; gossip encrypted+authenticated with a shared cluster secret (verified wrong-secret nodes cannot join). Per-node certs/mutual TLS remain M1b.
- **Web console seed BUILT ✅ (2026-07-18).** Embedded single-file console at `/ui`: live node health, latencies, recent routes; zero external assets.
- **Full console skeleton BUILT ✅ (2026-07-18).** Top menu with all doable capability sections (AI Services, Compute, Storage, Data & Messaging, Network, Security, Operations), every capability hyperlinked to its own page with status badge (Live / In progress / Planned) and principles note. Each page is the anchor for building that capability individually; still one embedded HTML file, own vocabulary only.

## Appendix A — open-source dependencies and compatibility targets (nominative references)
This appendix is the only place third-party names appear; all are open-source components we depend on or interoperate with, listed for engineering accuracy. None is part of our branding.

| Role | Component | License |
|---|---|---|
| Implementation language | Go | BSD-3 |
| Gossip membership library | memberlist | MPL-2.0 (fallback if relicensed: SWIM reimplementation or libp2p gossipsub) |
| Supervised inference runtimes | llama.cpp; Ollama | MIT; MIT |
| API wire format | de-facto industry chat-completions HTTP format | interoperability target, not a dependency |
| Overlay network (M5) | WireGuard; Headscale | GPLv2 (kernel) + BSD-3 |
| Accounting DB | SQLite | Public domain |
| Default model weights | Apache-2.0 open-weight families (e.g. Mistral 7B, Qwen2.5, OLMo 2) | Apache-2.0 |
| Model formats | GGUF, ONNX | open formats |

Rejected on licensing grounds: proprietary mesh-VPN control planes; BUSL/source-available orchestration and networking products; restricted-license model families as bundled defaults.
