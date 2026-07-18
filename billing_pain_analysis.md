# Cloud Features vs. Billing Pain — Implementation Order

Three inputs compared: (1) cloud features in current use and future scope, (2) the billing pain organizations actually report, (3) our board. Output: an ordered implementation list, cross-checked against existing issues.

## 1. Cloud features — in use now vs. future scope
**In heavy use today** (see cloud_service_coverage.md for the full 100): compute instances/containers, object storage, managed databases, serverless functions, CDN, queues/events, monitoring, IAM/KMS — and the fastest-growing segment, **AI APIs** (chat, embeddings, image, speech) metered per token.
**Future scope** (where cloud spend is heading): AI inference everywhere, GPU scarcity pricing, agentic workloads (many small autonomous API calls), edge compute, vector/RAG stacks, privacy-regulated data processing. **Every one of these makes billing *less* predictable** — usage-driven, bursty, and hard to attribute.

## 2. The billing pain (what people and orgs report)
| # | Pain | Mechanism |
|---|---|---|
| P1 | Unpredictable monthly bills | Per-second/per-token metering × autoscaling = invoices nobody can forecast |
| P2 | Egress fees | Data is free to bring in, expensive to take out — a lock-in tax on leaving |
| P3 | Idle/zombie spend | Paying for provisioned-but-unused instances, disks, IPs; est. ~30% of cloud spend wasted |
| P4 | Pricing complexity | Hundreds of SKUs, regions, tiers; whole FinOps teams exist just to read the bill |
| P5 | Surprise overruns | A bug, a retry loop, or a viral day turns into a 10–100× bill overnight |
| P6 | Per-token AI costs | LLM usage scales with success; costs grow faster than revenue for AI features |
| P7 | Reserved-instance gambling | Discounts require predicting your own usage years ahead |
| P8 | No cost attribution | Who spent it? Tagging is manual, incomplete, always behind |
| P9 | Free-tier traps | $0 to start, cliff pricing at scale; migration cost locks you in by then |
| P10 | Currency/regional markup | Non-US orgs pay FX premiums and regional surcharges |

## 3. The structural answer
Cloudless doesn't discount these pains — it deletes their mechanism: owned hardware has **zero marginal cost**. No meter (kills P1, P5, P6), no egress fee (P2 — data moves inside your own mesh), no SKUs (P4), no reservations (P7), no regional markup (P10). What remains genuinely needed — and becomes our work list — is **visibility and fairness**: knowing who contributed and consumed what (P3, P8), and keeping usage within group agreements (P5's group-scale analogue).

## 4. Ordered implementation list (cross-checked with board issues)
| Order | Work item | Answers pain | Board status |
|---|---|---|---|
| 1 | Usage accounting per key/node | P1, P8 | **#12 C2 — exists, P2 → promote to P1** |
| 2 | Quotas & rate limits | P5 | **#13 C3 — exists, P2 → promote to P1** |
| 3 | Per-user API keys | P8 | **#11 C1 — exists** |
| 4 | Contribution & consumption ledger (who gave/used compute; fairness view; seed of credits) | P3, P8 | **gap → new issue H1** |
| 5 | Cost-comparison calculator (what this mesh replaces in cloud spend, live on website+console) | P1, P4 | **gap → new issue H2** |
| 6 | Idle-capacity surfacing (show unused group capacity so it gets used, not wasted) | P3 | **gap → new issue H3** |
| 7 | Model commons (no re-download = no bandwidth waste) | P2-analogue | **#6–#10 Epic B — exists** |
| 8 | Batch/scheduled jobs (soak idle capacity) | P3 | **#24/#25 F5/F6 — exists** |
| 9 | Benchmarks vs cloud baselines (prove the economics publicly) | P1, P4 | **#16/#17 D1/D2 — exists** |
| 10 | Object store / backup (kill storage+egress bills) | P2, P9 | **#28/#31 F7/F8 — exists** |

**Cross-check verdict:** the board already covered 7 of 10; three genuine gaps (ledger, calculator, idle surfacing) become **Epic H — Billing Freedom**. C2/C3 get promoted to P1 because billing visibility is the pain users feel first.

## 5. Loop plan
Implement in the order above; each item closes its issue and updates the console. Items 1–2 (usage accounting + quotas) are next up.
