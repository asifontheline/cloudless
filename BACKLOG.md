# Open Feature Backlog

The open, public board lives on GitHub Issues/Milestones (our "Jira" — open tooling per
project principles). This file is the canonical snapshot. Status as of 2026-07-19.

**Legend:** ✅ shipped · 🔶 in progress · ⬜ planned. **Priority:** P1 current · P2 next · P3 backlog.

## EPIC A — Secure Mesh Foundation
| ID | Story | Status |
|---|---|---|
| #1 A1 | Cluster CA at init/up | ✅ |
| #2 A2 | Single-use expiring join tokens | ✅ |
| #3 A3 | Mutual TLS between nodes | ✅ |
| #4 A4 | Node revocation (cert refused + dropped, gossip-propagated) | ✅ |
| #5 A5 | Signed audit log | ✅ |
| #52 A6 | Passwordless sign-in (passkeys/WebAuthn) | ⬜ |

## EPIC B — Model Commons
| #6 B1 | Content-addressed model store | ✅ |
| #7 B2 | Mesh pull-through cache | ✅ |
| #8 B3 | Safe-format allowlist (reject pickle) | ✅ |
| #9 B4 | Model registry in console | ✅ (via Model Commons page) |
| #10 B5 | Runtime supervisor | ✅ |

## EPIC C — Accounts & Fair Use
| #11 C1 | Per-user API keys | ✅ |
| #12 C2 | Usage accounting | ✅ |
| #13 C3 | Quotas & rate limits | ✅ |
| #14 C4 | Request queueing | ✅ |
| #15 C5 | Mid-stream failover retry | ✅ |

## EPIC D — Evaluation & Paper
| #16 D1 | Churn test harness | ✅ |
| #17 D2 | Latency/throughput benchmarks | ✅ (`cloudless bench` — p50/p95/p99 latency, req/s, tok/s) |
| #18 D3 | Telemetry export | ✅ (`GET /metrics` — Prometheus-compatible text exposition) |
| #19 D4 | Paper §8 experiments | ⬜ |

## EPIC E — Network & Onboarding
| #20 E1 | Bundled encrypted overlay | ⬜ |
| #21 E2 | Join links/QR from console | ✅ |
| #22 E3 | Internal naming | ✅ (`GET /names`, `cloudless resolve` — nodes + extensions, one directory) |
| #23 E4 | Signed release binaries | 🔶 (cross-platform build pipeline shipped; actual signing blocked on acquiring paid Windows/Apple certs) |
| #67 | Merge-queue → deploy auto-trigger (token-cascade fix) | ✅ (inline FTPS deploy in the same job, no cross-workflow trigger needed) |

## EPIC F — Beyond Inference
| #24–#35 | Embeddings, speech, TTS, images, batch, scheduled, object store, backup, queues, vector search, anomaly quarantine, k-of-n validation | ⬜ |

## EPIC G — Encryption & Data Guard
| #36–#43 | Key hierarchy, at-rest encryption, zero-plaintext audit, data classification/locality, egress guard, movement audit, crypto-shredding, k-of-n key recovery | ⬜ |

## EPIC H — Billing Freedom ✅ complete
| #44 H1 | Contribution & consumption ledger | ✅ |
| #45 H2 | Cost-comparison calculator | ✅ |
| #46 H3 | Idle-capacity surfacing | ✅ |

## EPIC I — Community Fabric
| #47 I1 | Per-node resource share controls (5% default → 70% ceiling) | ✅ |
| #48 I2 | Reciprocity: contribution-based entitlement | 🔶 (`/ledger` reports an advisory entitlement hint per contributing node; no binding exists today between a node's identity and an individual user's API key, so this informs an admin's manual `cloudless keys` decision rather than auto-enforcing anything) |
| #49 I3 | Geo network map | 🔶 (map live; enrichment ongoing) |
| #50 I4 | Locality-aware redundancy & routing | ✅ (redundancy via M1's domain-diversified placement; routing now prefers nearby healthy peers over raw latency) |

## EPIC J — Mobile Nodes
| #53 J1 | Mobile node agent (Android & iOS) | ⬜ |
| #54 J2 | Thermal & battery safety guard | ⬜ |
| #55 J3 | Tunable cap 5%→70% (all nodes) | ✅ |
| #56 J4 | Mobile portal (passkey PWA) | ⬜ |
| #57 J5 | Mobile packaging & distribution | ⬜ |

## EPIC K — Open Platform & Polyglot
| #58 K1 | Stable open API specification | ✅ (PROTOCOL.md + formal spec served at /openapi.yaml) |
| #59 K2 | Python SDK (first-class) | ✅ |
| #60 K3 | JavaScript/TypeScript SDK | ✅ (zero-dep fetch client + .d.ts; parity with Python) |
| #61 K4 | Polyglot extension model | ✅ (HTTP-register V1; WASM sandbox later) |
| #62 K5 | Polyglot runtime backends | ✅ (`cloudless ext -inference add` — any-language service joins the `/v1/chat/completions` routing pool, not just `/x/...` proxying) |

## EPIC L — Test & Quality
| #84 L1 | Regression test cases for every shipped feature | 🔶 (registry, relay, store, gateway backfilled; more packages remain) |
| #85 L2 | Multi-node end-to-end mesh test in CI | ✅ |
| #86 L3 | Pre-merge gate — re-run tests against latest main just before merging | ✅ |
| #87 L4 | SDK conformance test cases (Python & JS) against a live node | ✅ |
| #88 L5 | Tests-required policy for all future features | ✅ (soft coverage gate; hardens with L1) |
| #89 L6 | Browser test cases — console & website smoke tests | ⬜ |
| #90 L7 | Security regression test cases | 🔶 (revoked-node routing eviction + admin-key gating, path-traversal-name rejection on store/vault; not a full endpoint-by-endpoint audit) |

## EPIC M — Data Durability & Recovery (MUST-DO)
Node churn must never mean lost or breached data. Prerequisite for Epic N recruitment.
| #92 M1 | N-copy replication across failure domains | ✅ |
| #93 M2 | Self-healing re-replication on node loss | ✅ |
| #94 M3 | Encrypt before data leaves the owner's machine (breach containment) | ✅ |
| #95 M4 | Restore lost data — owner-initiated recovery flow | ✅ |
| #96 M5 | Off-mesh backup export & re-import (escape hatch) | ✅ |
| #97 M6 | Measured durability guarantees on the console | ✅ |
| #108 M7 | Temperature-tiered storage compression (hot fast · cold small) | ✅ |

## EPIC N — Mesh Expansion & Node Hosting (PRIMARY growth path)
The primary path to expand the mesh: recruit free and willing node hosts. Gated on M1–M3.
| #98 N1 | **PRIMARY** — recruit homelab & self-hosting communities | ⬜ P1 |
| #99 N2 | Always-free cloud tier seed nodes | ⬜ P2 |
| #100 N3 | Grant-funded and OSS-credit seed hosting | ⬜ P2 |
| #101 N4 | Universities, hackerspaces & computer clubs | ⬜ P3 |

## EPIC O — Speed by Divide & Conquer
Individual machines are modest; the mesh is not. Speed comes from dividing work across nodes.
| #102 O1 | Parallel fan-out — split batch work across nodes, merge results | ✅ |
| #103 O2 | Speculative racing — first answer wins | ✅ |
| #104 O3 | Model sharding — run models no single node can | ⬜ P2 |
| #105 O4 | Chunked parallel transfers from many peers | ✅ (`/store/pull` splits large artifacts across every peer that holds them) |
| #106 O5 | Divide-and-conquer batch jobs — map, process, merge | ✅ (`POST /v1/map` — one template applied across many chunks, fanned out via O1, merged in order) |
| #107 O6 | Speed-aware scheduling & honest speed-up metrics | ✅ (`/v1/batch` reports measured elapsed vs. summed sequential time — real speedup, not a claimed number) |
| #109 O7 | Transfer compression — compress on the wire, decompress at receiver | ✅ |

## EPIC P — Mesh Cloud Offerings (next wave; after current epics)
This epic family captures the public-cloud-style service catalog and names it in mesh-native terms. The full catalog lives in [PC2Meshoffering.md](PC2Meshoffering.md).
| #110 P1 | Mesh Compute & Functions — runtime, scheduling, sandboxed execution, triggers | ⬜ P2 |
| #111 P2 | Mesh Storage & Recovery — object vault, snapshots, archive, restoration | ⬜ P2 |
| #112 P3 | Mesh Data Fabric — database, key-value, document, and cache services | ⬜ P2 |
| #113 P4 | Mesh Transit & Edge — overlay routing, discovery, ingress, edge cache | ⬜ P3 |
| #114 P5 | Mesh AI Fabric — model endpoints, evaluation, and training hooks | ⬜ P3 |
| #115 P6 | Mesh Queue & Integration — topics, queues, event bus, and connectors | ⬜ P3 |
| #116 P7 | Mesh Identity & Security — secrets, key hierarchy, policy enforcement | ⬜ P3 |
| #117 P8 | Mesh Ops & Observability — metrics, logs, traces, alerts | ⬜ P3 |
| #118 P9 | Mesh DevOps — build, release, package, and environment provisioning | ⬜ P3 |
| #119 P10 | Mesh Data Lake & Analytics — ETL, streaming, and query services | ⬜ P3 |
| #120 P11 | Mesh Edge Relay & IoT — device onboarding, telemetry, and edge execution | ⬜ P3 |

## EPIC Q — End-User CLI
The `cloudless` binary today is operator-shaped (up, serve, keys, vault, backup, revoke...). This
epic is the consumer-facing counterpart: a scriptable command surface for people who just want to
*use* a mesh — send a prompt, pipe data through it, wire it into a shell script or CI job — without
writing code against the Python/JS SDKs (K2/K3) or hand-rolling curl calls. Same wire API underneath;
this is ergonomics, not new capability.
| ID | Story | Status |
|---|---|---|
| #121 Q1 | `cloudless chat` — one-shot and interactive prompts, streamed to the terminal | ✅ |
| #122 Q2 | Stdin/stdout piping — prompt from stdin, completion to stdout, composes in shell pipelines | ✅ (shipped with Q1 — piped stdin auto-detected) |
| #123 Q3 | Uniform `--format json\|table\|plain` across every command, for scripting | ✅ (`table`\|`json`\|`plain` now on every command with structured output: status, resolve, usage, capacity, savings, ledger, keys, ext, vault, models, share, nodes, audit — `plain` is headerless tab-separated, verified scriptable end-to-end with a real `cut -f1` pipeline) |
| #124 Q4 | Multi-profile config (`cloudless config set/get/use`) — switch between meshes without re-flagging `-addr`/`-key` every time | ✅ (`resolveAddrKey` wired into every command that talks to a gateway) |
| #125 Q5 | `CLOUDLESS_API_KEY` env var + documented exit-code conventions, so CI/scripts never need the key on the command line or in shell history | ✅ (same `resolveAddrKey` path as Q4, so every command inherits it; exit codes documented) |
| #126 Q6 | Shell completion (bash/zsh/fish) | ✅ (`cloudless completion bash\|zsh\|fish`) |

## EPIC R — Frictionless Joining
Grounded in a real onboarding session, not guesswork: getting a second machine onto an existing
mesh hit PowerShell PATH not refreshing after install, a clone folder created somewhere unwritable,
Windows Smart App Control silently blocking the binary, WSL2's default NAT networking isolating the
node from the mesh entirely, and a join-link UI error ("admin key required") that turned out to be a
localStorage casing typo the user couldn't see without opening DevTools. Each of those got a docs fix
in the moment; this epic is about the product closing those gaps itself instead of needing a human to
walk someone through it next time.
| ID | Story | Status |
|---|---|---|
| #127 R1 | `cloudless join <link>` — accept the full join link/URL directly (as generated by E2), no manual secret@host:port assembly | ⬜ P1 |
| #128 R2 | Pre-flight connectivity check before attempting to join — plain-language diagnosis (unreachable, wrong secret, TLS mismatch, port blocked) instead of a raw Go dial error | ✅ (`diagnoseJoinError` classifies the real enrollment attempt's failure — timeout, DNS, connection refused — into a plain-language cause; non-network errors like a rejected join token already had their own clear message and pass through unchanged) |
| #129 R3 | Post-join confirmation — after `up -join` succeeds, verify the new node actually shows healthy in mesh `/status` and say so explicitly; don't let "the process didn't crash" pass for "you're in" | ✅ (5s after joining, an explicit `✓ connected — N peer(s) visible` or `⚠ NOT CONNECTED` log line — closes the exact silent-standalone gap from this session's WSL onboarding incident) |
| #130 R4 | Proactive OS security-gate detection — recognize Windows Smart App Control / Device Guard and macOS Gatekeeper blocks and explain the one-step fix immediately, instead of a cryptic exec failure | ⬜ P2 |
| #131 R5 | WSL2 networking guidance built into `up` on Windows — detect default NAT mode and either warn with the exact `.wslconfig` fix or (stretch) offer to apply it | ⬜ P2 |
| #132 R6 | Guided interactive first-run — `cloudless up` with no flags and no existing config prompts step-by-step instead of assuming the operator already knows the right flags | ⬜ P2 |
| #133 R7 | Join-link admin key errors are diagnosable without DevTools — the console surfaces what key it actually sent, not just "admin key required" | ✅ (`adminKeyHint()` distinguishes "no key saved" from "saved key was rejected" across every admin action: revoke, restore, share, keys, join-link) |

## EPIC S — Security Operations & Attack Resilience
The mesh already has real defenses baked into its foundation — mTLS between nodes (A3), single-use
join tokens (A2), node revocation (A4), a hash-chained audit log (A5), a safe-format allowlist that
rejects pickle-based model files outright (B3), and per-key rate limits (C3). This epic is the
operational layer on top: catching attacks *in progress*, not just admitting the right nodes and
rejecting the wrong file formats. Scope is deliberately narrow to what a small mesh operator can
actually act on — this is not a SOC. Never claim "foolproof"; layered defense only, matching this
project's existing security posture.
| ID | Story | Status |
|---|---|---|
| #134 S1 | Dependency vulnerability scanning in CI (`govulncheck`) — known CVEs in our own dependency tree caught before merge, not discovered after | ✅ (runs in both CI validation and the merge queue's pre-merge revalidation; currently clean) |
| #135 S2 | Abuse-pattern quarantine — a key or node showing attack-shaped behavior (mass auth failures, request flooding past quota, scanning for endpoints) is automatically flagged and rate-limited harder, not just counted | ✅ (`internal/abuse` — 10 failed attempts/min from one source locks it out for 5 minutes, on both the per-user and admin auth paths; the correct key is rejected too while locked out, since the lockout is about the source, not any one credential) |
| #136 S3 | Security incident response runbook — documented, concrete steps for a leaked cluster secret, a compromised node, or a malicious extension registration; who revokes what, in what order | ✅ (`security_architecture.md` — verified every command against the actual CLI; honestly flags that in-place cluster-secret rotation is a real gap, not yet built) |
| #137 S4 | Extension network isolation guidance — K4/K5 extensions run under their own OS permissions already (the gateway proxies, never executes); document and, where possible, enforce that they shouldn't have unrestricted outbound network access by default | ✅ (`security_architecture.md`, Layer 3 — verified both the `/x/` proxy path and K5's inference-routing path never forward mesh credentials to an extension; honestly states network egress isolation is an operator deployment choice, not something the gateway can enforce on a process it doesn't spawn) |
| #138 S5 | Audit-log tamper alerting — `Verify()` already detects a broken hash chain (A5); when it does, the node should say so loudly (console banner, non-zero health check) instead of only surfacing it to whoever happens to query `/audit` | ✅ (`GET /healthz` returns 503 + a logged warning when the audit chain is broken; mesh routing is unaffected — peer health probes hit the backend's own `/models`, not this endpoint) |
| #139 S6 | Gossip message rate-limiting — an authenticated-but-compromised peer flooding gossip traffic shouldn't be able to degrade the mesh for everyone else | ✅ (token-bucket in the gossip delegate — 50 burst, 10/s refill; found the concrete threat while writing this: revoke.Set.Add persists to disk on every distinct fabricated name, so this was a real unbounded disk-I/O and memory vector, not a hypothetical one) |
| #140 S7 | Release artifact provenance — checksums (and, once E4 ships, signatures) published alongside every release binary, documented verification steps for anyone downloading one | ✅ (already true of the E4 pipeline: `sha256sum` generated per binary, self-verified in CI before publishing, verification command in every release's notes; signatures remain blocked on the same paid certs as the rest of E4) |

## EPIC T — Extended Communication Channels & Devices
Today every node talks over plain TCP/UDP sockets — gossip membership, mutual-TLS peer
traffic, the gateway's HTTP API — which is transport-agnostic in practice (it already
works over Ethernet, Wi-Fi, cellular, satellite, or a VPN overlay, unchanged) but
transport-*blind* in design: nothing in the mesh knows or cares which medium a link
actually runs over, and there's no path today for a device with no IP network at all —
a phone with only Bluetooth to a nearby node, a sensor with only LoRa, two machines in
the same room with no router between them. This epic is about making the transport
layer pluggable and adding the mediums that matter for real deployment gaps, not
protocol novelty for its own sake.

**Candidate mediums (reference list — extend as new ones become relevant):**

*Wired:*
- Ethernet (copper, twisted pair) — Cat5e/6/7, most common LAN medium, up to ~10Gbps+
- Fiber optic — light pulses through glass/plastic fiber, longest range + highest bandwidth, immune to EM interference
- Coaxial cable — cable internet (DOCSIS), older Ethernet (10BASE2)
- Powerline (PLC) — data over existing electrical wiring (e.g. HomePlug)
- USB — short-range device-to-device/host
- Serial/RS-232, RS-485 — legacy and industrial control systems
- Thunderbolt — high-speed peripheral/display, can carry networking too

*Wireless:*
- Wi-Fi (802.11) — LAN-range RF, most common wireless network medium
- Bluetooth / BLE — short-range, low-power, device pairing
- Cellular (4G/5G/LTE) — wide-area, carrier infrastructure
- Satellite — global coverage, high latency (except newer LEO constellations like Starlink)
- NFC — very short range (cm), tap-to-connect
- Zigbee / Z-Wave / Thread — low-power mesh, IoT/home automation
- LoRa / LoRaWAN — long-range, low-bandwidth, low-power (IoT, sensors)
- Infrared (IR) — line-of-sight, largely legacy (old remotes)
- Microwave / point-to-point RF links — fixed wireless, backhaul
- Ultra-wideband (UWB) — precise short-range ranging/positioning (e.g. AirTag-style)

**Current state:** the gossip mesh and gateway run over standard TCP/UDP sockets, so
they're already transport-*medium*-agnostic — nodes can be linked by any of the wired
or wireless mediums above (Ethernet, Wi-Fi, cellular, even a Tailscale/VPN overlay
across the internet) with zero code changes, as long as IP connectivity exists between
them. What's actually missing (T1-T7 below) is support for mediums that *don't* carry
IP on their own — Bluetooth, LoRa, NFC, USB/serial, offline media — which need an
explicit transport layer, not just a cable plugged in. Any future medium worth
supporting should be added to this reference list first, then given its own story.
| ID | Story | Status |
|---|---|---|
| #141 T1 | Transport abstraction layer — decouple gossip and the gateway's peer traffic from raw TCP so alternate transports can plug in without touching mesh/routing logic | ⬜ P2 |
| #142 T2 | Bluetooth/BLE local transport — proximity nodes (especially mobile, J epic) discover and link without any existing IP infrastructure in the room | ⬜ P2 |
| #143 T3 | LoRa/LoRaWAN long-range low-power transport — remote or rural nodes with no broadband, trading bandwidth for reach | ⬜ P3 |
| #144 T4 | Offline store-and-forward sync ("sneakernet") — bridge two disconnected mesh segments via removable media when no live link exists between them at all | ⬜ P3 |
| #145 T5 | Direct USB/serial link support — air-gapped or embedded/constrained devices with no network stack to speak of | ⬜ P3 |
| #146 T6 | NFC tap-to-join — physical-proximity onboarding alongside E2's existing QR/join-link flow, for devices where scanning a code is awkward | ⬜ P3 |
| #147 T7 | Transport health & cost awareness — surface which medium a link actually runs over (Wi-Fi/cellular/satellite/LoRa/...) and its latency/cost profile, so routing (already health- and locality-aware, I4) can factor it in | ⬜ P2 |

## Cross-cutting infrastructure (shipped)
- One-command onboarding (`up`), encrypted gossip mesh, failover gateway, embedded web console ✅
- CI validation engine + branch-protected `main` + 2-hourly review-gated merge queue ✅
- Website auto-published to cldless.com; contributor guide + open protocol published ✅
- Website feature search — a real B+ tree keyword index over every feature's title and
  description, plus an extensible synonym layer so a related-but-not-literal query (e.g.
  "database" or "security") surfaces the right feature even with no word overlap; verified
  with 11 real functional tests against the actual site's feature data (not a DOM/browser
  test — the search logic itself, executed) ✅

## Working agreements
- **No direct commits to main** — branch → PR → CI validation → review (`ready-to-merge`) → merge queue.
- Every story ships web console + HTTP API + CLI together.
- Security acceptance criteria are part of "done".
- OSI-approved licenses only; own vocabulary only (no proprietary names); safe-by-default; master-less.
