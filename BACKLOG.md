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
| #10 B5 | Runtime supervisor | ⬜ |

## EPIC C — Accounts & Fair Use
| #11 C1 | Per-user API keys | ✅ |
| #12 C2 | Usage accounting | ✅ |
| #13 C3 | Quotas & rate limits | ✅ |
| #14 C4 | Request queueing | ✅ |
| #15 C5 | Mid-stream failover retry | ✅ |

## EPIC D — Evaluation & Paper
| #16 D1 | Churn test harness | ⬜ |
| #17 D2 | Latency/throughput benchmarks | ⬜ |
| #18 D3 | Telemetry export | ⬜ |
| #19 D4 | Paper §8 experiments | ⬜ |

## EPIC E — Network & Onboarding
| #20 E1 | Bundled encrypted overlay | ⬜ |
| #21 E2 | Join links/QR from console | ✅ |
| #22 E3 | Internal naming | ⬜ |
| #23 E4 | Signed release binaries | ⬜ |
| #67 | Merge-queue → deploy auto-trigger (token-cascade fix) | ⬜ |

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
| #48 I2 | Reciprocity: contribution-based entitlement | ⬜ |
| #49 I3 | Geo network map | 🔶 (map live; enrichment ongoing) |
| #50 I4 | Locality-aware redundancy & routing | ⬜ |

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
| #62 K5 | Polyglot runtime backends | ⬜ |

## EPIC L — Test & Quality
| #84 L1 | Regression test cases for every shipped feature | 🔶 (registry, relay, store, gateway backfilled; more packages remain) |
| #85 L2 | Multi-node end-to-end mesh test in CI | ✅ |
| #86 L3 | Pre-merge gate — re-run tests against latest main just before merging | ✅ |
| #87 L4 | SDK conformance test cases (Python & JS) against a live node | ✅ |
| #88 L5 | Tests-required policy for all future features | ✅ (soft coverage gate; hardens with L1) |
| #89 L6 | Browser test cases — console & website smoke tests | ⬜ |
| #90 L7 | Security regression test cases | ⬜ |

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
| #105 O4 | Chunked parallel transfers from many peers | ⬜ P2 |
| #106 O5 | Divide-and-conquer batch jobs — map, process, merge | ⬜ P2 |
| #107 O6 | Speed-aware scheduling & honest speed-up metrics | ⬜ P2 |
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

## Cross-cutting infrastructure (shipped)
- One-command onboarding (`up`), encrypted gossip mesh, failover gateway, embedded web console ✅
- CI validation engine + branch-protected `main` + 2-hourly review-gated merge queue ✅
- Website auto-published to cldless.com; contributor guide + open protocol published ✅

## Working agreements
- **No direct commits to main** — branch → PR → CI validation → review (`ready-to-merge`) → merge queue.
- Every story ships web console + HTTP API + CLI together.
- Security acceptance criteria are part of "done".
- OSI-approved licenses only; own vocabulary only (no proprietary names); safe-by-default; master-less.
