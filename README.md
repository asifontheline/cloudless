# Cloudless

**An alternative to commercial cloud — a community mesh network spread across continents with no data centres, running on commodity hardware and the systems people already own.**

Cloudless is the parent platform: independent machines — from a spare desktop to a phone in someone's pocket, anywhere on earth — federate their compute, storage, and bandwidth into one mesh that behaves like a cloud, owned by the people who run it, with none of a cloud's bills, lock-in, or data custody.

## Philosophy

Commercial clouds win by concentrating expensive hardware in their data centers and renting it back to you. Cloudless inverts that: the machines people already own — spare desktops, old workstations, consumer GPUs — are federated into shared infrastructure with replication, failover, and web-based management built in. Privacy-first (data never leaves group hardware), cooperatively owned (no vendor), and honest about trade-offs (defense in depth, not "foolproof"; group scale, not hyperscale).

## Product family

```
Cloudless (parent) — the community mesh platform
│   node agent · gossip membership · gateway/failover · web console · security backbone
│
├── Cloud AI (first child, current focus)
│     chat & completions · embeddings · speech · vision · image generation · vector search
├── Cloud Storage (child, planned)
│     model commons · object store · backup & archive
├── Cloud Compute (child, planned)
│     batch jobs · scheduled jobs · containers · functions
└── Cloud Data (child, planned)
      queues & events · key-value · time-series · workflows
```

Cloud AI is where we start because it is where cost and privacy pain are highest today — but the mesh underneath is general, and every child service rides the same nodes, the same security backbone, and the same console.

## Status

Working prototype: one-command onboarding (`up`), encrypted gossip mesh, standards-compatible inference gateway with latency-ranked routing and automatic failover, embedded web console. See [mvp_design.md](mvp_design.md), [security_architecture.md](security_architecture.md), and the open board: [issues](../../issues) · [milestones](../../milestones) · [BACKLOG.md](BACKLOG.md).

## Principles (hard constraints)

1. **Cheap community hardware first** — heterogeneous, low-cost machines are the backbone, not a compromise.
2. **Open source only** — OSI-approved licenses throughout; no proprietary control planes; no restricted-license models bundled.
3. **Web-managed, zero-friction** — everything manageable from the embedded console; setup measured in seconds, not steps.
4. **Security as a backbone** — layered defense: identity + revocation, mutual TLS, sandboxing, hash-verified artifacts, audit log.
5. **Own vocabulary** — we replicate cloud capabilities, never cloud branding.

License: Apache-2.0 (working title "cloudless"; final name pending).
