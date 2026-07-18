# Open Feature Backlog

The open, public board for this project lives on GitHub Issues/Milestones (our "Jira" — open tooling per project principles). This file is the canonical backlog snapshot; each story below becomes a GitHub issue.

**Priority:** P1 = current milestone · P2 = next · P3 = backlog · Status reflects prototype as of 2026-07-18.

## EPIC A — Secure Mesh Foundation (M1b) — P1
| ID | Story | Acceptance criteria |
|---|---|---|
| A1 | Cluster CA at `init`/`up` | CA key generated locally; node certs issued; docs updated |
| A2 | Single-use expiring join tokens | Token mints from console/CLI; second use rejected; TTL enforced |
| A3 | Mutual TLS between nodes | All peer traffic mTLS; plaintext peer port removed |
| A4 | Node revocation | One console action evicts node: cert revoked + gossip key rotated |
| A5 | Signed audit log | Admin actions append-only + signature-chained; visible in console |

## EPIC B — Model Commons (M2) — P1
| B1 | Content-addressed model store | Blobs stored/served by SHA-256; hash verified on receive & load |
| B2 | Mesh pull-through cache | `models pull` checks peers before public repositories |
| B3 | Safe-format allowlist | GGUF/safetensors/ONNX only; pickle-based files rejected with clear error |
| B4 | Model registry in console | List/pull/delete models per node from the browser |
| B5 | Runtime supervisor | Agent launches/monitors local inference server; model load/unload API |

## EPIC C — Accounts & Fair Use (M3) — P2
| C1 | Per-user API keys | Create/revoke in console; keys scoped to models |
| C2 | Usage accounting | Tokens per key/node in embedded DB; console usage page |
| C3 | Quotas & rate limits | Per-key limits enforced at gateway |
| C4 | Request queueing | Backpressure instead of errors under load |
| C5 | Mid-stream failover retry | Retry before first token; document semantics |

## EPIC D — Evaluation & Paper (M4) — P2
| D1 | Churn test harness | Scripted node kill/join; availability measured |
| D2 | Latency/throughput benchmarks | vs single-node and hosted-API baselines |
| D3 | Telemetry export | Standard metrics format from every node |
| D4 | Paper §8 experiments | Results written back into cloudless_paper.md |

## EPIC E — Network & Onboarding (M5) — P3
| E1 | Bundled encrypted overlay | Nodes connect across NAT without third-party setup |
| E2 | Join links/QR from console | Browser-generated invite encodes token+endpoint |
| E3 | Internal naming | Stable service names inside the mesh |
| E4 | Signed release binaries | Reproducible builds; checksums published |

## EPIC F — Beyond Inference (from coverage matrix) — P3
| F1 | Embeddings endpoint | `/v1/embeddings` routed like chat |
| F2 | Speech-to-text service | Transcription runtime behind gateway |
| F3 | Text-to-speech service | Voice synthesis on capable nodes |
| F4 | Image generation jobs | GPU-node job type with queue |
| F5 | Batch jobs | Parallel task fan-out (transcode, data prep) |
| F6 | Scheduled jobs | Cron-style with failover |
| F7 | Object store | Replicated content-addressed general storage |
| F8 | Backup & archive | Encrypted k-of-n backups on community disks |
| F9 | Queues & events | Durable messaging substrate |
| F10 | Vector search | Semantic store paired with inference |
| F11 | Anomaly quarantine | Behavioral signals drain suspicious nodes |
| F12 | k-of-n result validation | Redundant execution + comparison for critical jobs |

## Working agreements
- Every story ships web console + HTTP API + CLI together (one-surface rule).
- Security acceptance criteria are part of "done", not a later pass.
- All naming follows the no-proprietary-names policy; licenses checked before any new dependency.
