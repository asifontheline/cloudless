# Open Feature Backlog

The open, public board lives on GitHub Issues/Milestones (our "Jira" — open tooling per
project principles). This file is the canonical snapshot. Status as of 2026-07-19.

**Legend:** ✅ shipped · 🔶 in progress · ⬜ planned. **Priority:** P1 current · P2 next · P3 backlog.

## EPIC A — Secure Mesh Foundation
| ID | Story | Status |
|---|---|---|
| #1 A1 | Cluster CA at init/up | ✅ |
| #2 A2 | Single-use expiring join tokens | 🔶 (HMAC enrollment live; single-use tokens pending) |
| #3 A3 | Mutual TLS between nodes | ✅ |
| #4 A4 | Node revocation (cert refused + dropped, gossip-propagated) | ✅ |
| #5 A5 | Signed audit log | ⬜ |
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
| #14 C4 | Request queueing | ⬜ |
| #15 C5 | Mid-stream failover retry | ⬜ |

## EPIC D — Evaluation & Paper
| #16 D1 | Churn test harness | ⬜ |
| #17 D2 | Latency/throughput benchmarks | ⬜ |
| #18 D3 | Telemetry export | ⬜ |
| #19 D4 | Paper §8 experiments | ⬜ |

## EPIC E — Network & Onboarding
| #20 E1 | Bundled encrypted overlay | ⬜ |
| #21 E2 | Join links/QR from console | ⬜ |
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
| #58 K1 | Stable open API specification | ⬜ (PROTOCOL.md published; formal OpenAPI pending) |
| #59 K2 | Python SDK (first-class) | ✅ |
| #60 K3 | JavaScript/TypeScript SDK | ⬜ |
| #61 K4 | Polyglot extension model | ⬜ |
| #62 K5 | Polyglot runtime backends | ⬜ |

## Cross-cutting infrastructure (shipped)
- One-command onboarding (`up`), encrypted gossip mesh, failover gateway, embedded web console ✅
- CI validation engine + branch-protected `main` + 2-hourly review-gated merge queue ✅
- Website auto-published to cldless.com; contributor guide + open protocol published ✅

## Working agreements
- **No direct commits to main** — branch → PR → CI validation → review (`ready-to-merge`) → merge queue.
- Every story ships web console + HTTP API + CLI together.
- Security acceptance criteria are part of "done".
- OSI-approved licenses only; own vocabulary only (no proprietary names); safe-by-default; master-less.
