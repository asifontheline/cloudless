# Outreach Playbook — foundations, funders, and community growth

A reusable use case for pitching Cloudless to open-source foundations, public-interest
funders, and developer communities. One core pitch, tailored per audience, with concrete
asks and readiness steps. Tracks with issue #75 (governance evaluation).

---

## 1. The core use case (the pitch everyone gets)

**Problem.** Cloud compute — especially AI inference — is concentrated in a handful of
commercial providers. Costs are rising, data custody is theirs, and communities,
schools, labs, and small teams are priced out or locked in. Meanwhile the hardware
people already own sits idle: spare desktops, old workstations, consumer GPUs, phones.

**Solution.** Cloudless federates the machines people already own into one mesh that
behaves like a cloud — replication, failover, latency-ranked routing, and web-based
management built in — owned by the people who run it. Privacy-first (data never leaves
group hardware), cooperatively owned (no vendor), honest about trade-offs (defense in
depth, not "foolproof"; group scale, not hyperscale).

**Proof it works today.** Working prototype:
- One-command onboarding (`up`) with an embedded web console — zero-setup management.
- Encrypted gossip membership, standards-compatible inference gateway with
  latency-ranked routing, automatic failover, and backpressure.
- Security backbone: identity + revocation, mutual TLS, hash-verified artifacts,
  signed (hash-chained) audit log, quarantine.
- Safe-by-default contribution: every node donates 5% by default, user-tunable with a
  hard ceiling, thermal/battery guard planned for mobile.
- Polyglot: Go core, Python and JavaScript/TypeScript SDKs, open protocol
  ([PROTOCOL.md](PROTOCOL.md)).
- Apache-2.0, OSI-approved licenses throughout — a hard constraint, not a preference.

**Why it matters beyond us.** This is public-interest infrastructure: it lowers the
cost floor for AI and cloud capability to "hardware you already own," keeps data under
community custody, and demonstrates that cloud capabilities can be replicated as a
commons rather than rented.

**The one-liner.** *A community mesh that turns the computers people already own into a
cooperatively owned cloud — starting with AI inference.*

---

## 2. Per-audience pitches

Each entry: the angle that lands with that audience, what we ask, what we offer, and
first contact.

### CNCF (Linux Foundation) — Sandbox

- **Angle.** Cloudless is cloud-native infrastructure built from first principles:
  gateway, service discovery via gossip, mTLS identity, failover, observability — but
  targeting community-owned hardware instead of data centers. Sandbox exists exactly
  for early, novel takes on cloud infrastructure.
- **Ask.** Sandbox acceptance: neutral home, visibility, contributor pipeline.
- **Offer.** A differentiated project (community mesh vs. data-center orchestration),
  active development, Apache-2.0, open governance willingness.
- **First contact.** Sandbox application via the CNCF TOC process
  (https://github.com/cncf/sandbox). Attend a TOC call; find a TOC sponsor familiar
  with edge/decentralized compute.

### LF AI & Data Foundation

- **Angle.** Open AI infrastructure is their whole remit. Cloudless makes AI inference
  a commons: standards-compatible serving across federated community hardware, no
  restricted-license models bundled, no proprietary control plane.
- **Ask.** Incubation-stage membership; landscape listing as a near-term step.
- **Offer.** A serving/inference project that fills a gap in their landscape
  (community-federated inference), cross-promotion with their projects.
- **First contact.** Project proposal to the LF AI & Data TAC
  (https://github.com/lfai/proposing-projects).

### Apache Software Foundation — Incubator (baseline, issue #75)

- **Angle.** Mature, vendor-neutral governance; strong brand for infrastructure.
- **Ask.** Incubation with a champion + mentors.
- **Trade-off.** Heavyweight process (IP clearance, release votes, mentor bandwidth);
  culture skews toward its established ecosystem. Keep as the compared baseline.
- **First contact.** Find a champion on general@incubator.apache.org; draft a proposal
  per https://incubator.apache.org/guides/proposal.html.

### Eclipse Foundation

- **Angle.** Strong in Europe, lighter process than ASF, proven with distributed and
  edge projects; good fit if European institutional users matter early.
- **Ask.** Project hosting under an appropriate top-level project or working group.
- **First contact.** https://www.eclipse.org/projects/handbook/ (project proposal).

### Commons Conservancy (NL)

- **Angle.** Minimal-overhead legal home; pairs naturally with NLnet funding. Keeps
  governance with the project while providing a legal entity for assets and donations.
- **Ask.** Hosting as a Programme.
- **First contact.** https://commonsconservancy.org/join/.

### NLnet Foundation / NGI Zero — funding (highest probability, apply first)

- **Angle.** EU-backed grants for open internet infrastructure; decentralized,
  privacy-first compute is squarely in remit; they fund individual developers, no
  entity required. Typical grants €5k–€50k.
- **Ask.** Grant for a scoped milestone — e.g., mobile node agent with thermal/battery
  guard (Epic J), or the storage child service.
- **Offer.** Everything they require is already true: OSI licenses, open protocol,
  public repo, security-conscious design.
- **First contact.** https://nlnet.nl/propose/ — open calls roughly every two months.
  The application is short (~2 pages); reuse §1 verbatim.

### Sovereign Tech Agency (DE) — funding

- **Angle.** Funds critical open infrastructure maintenance and resilience. Pitch the
  security backbone and mesh resilience as digital-sovereignty infrastructure.
- **Ask.** Fund hardening: threat-model review, fuzzing, protocol audit.
- **First contact.** https://www.sovereign.tech/apply.

### Fiscal hosting — Open Collective (now), SFC / SPI (later)

- **Angle.** The donations tab needs a transparent, legal way to receive money.
  Open Collective costs nothing to start and requires no governance changes; Software
  Freedom Conservancy or Software in the Public Interest are heavier options if/when
  assets grow.
- **Ask.** Open Source Collective as fiscal host.
- **First contact.** https://opencollective.com/create — same day setup.

### Mozilla / Prototype Fund / GitHub programs — opportunistic

- Mozilla periodically funds open AI infrastructure (watch their calls).
- Prototype Fund: requires a German connection; €47.5k for 6 months if applicable.
- GitHub Sponsors: enable alongside Open Collective; GitHub Accelerator when a cohort
  opens.

### Community developers — the contributor pitch

- **Angle.** "Build the cloud you co-own." Contributors get: a codebase small enough
  to hold in your head (Go core + thin SDKs), an open protocol to implement in any
  language, epics with real scope (mobile agent, storage, compute children), and a
  no-CLA Apache-2.0 project.
- **Channels.**
  - **FOSDEM** — distributed computing and AI devrooms; submit a talk built from §1.
  - **Google Summer of Code** — needs an umbrella org first (foundation above) or
    apply as an independent org; prepare 3–5 mentored project ideas from BACKLOG.md.
  - **Hacktoberfest** — label issues `good first issue` / `hacktoberfest` in September.
  - **Show-and-tell posts** — a working "spare laptops become an inference cluster in
    60 seconds" demo is the growth asset; record it once, reuse everywhere.
- **Readiness.** Good-first-issue labels, CONTRIBUTING.md walkthrough tested on a
  stranger, a public roadmap (milestones already exist), and a chat space linked from
  the console and README.

---

## 3. Sequencing

| When | Action | Why first |
|---|---|---|
| Now | Open Collective + GitHub Sponsors | Zero cost, unblocks donations |
| Now | NLnet application (next open call) | Highest probability money, no strings |
| Next | CNCF Sandbox **or** LF AI & Data proposal | Governance home; pick one, don't run both |
| Next | FOSDEM talk submission + demo video | Contributor pipeline |
| Later | GSoC (needs org/umbrella), Sovereign Tech | Depend on the above landing |
| Baseline | ASF Incubator comparison (issue #75) | Decide once CNCF/LF answer |

**Pick-one rule.** Foundations expect exclusivity of governance home. Apply to funders
(NLnet, Sovereign Tech) and fiscal hosts freely — those stack; governance homes don't.

---

## 4. Reusable assets to prepare

1. **Two-page PDF** of §1 + architecture diagram — every application asks for it.
2. **90-second demo video** — `up` on two machines, console shows the mesh, a request
   fails over live. The single highest-leverage asset for every audience.
3. **Roadmap link** — milestones + BACKLOG.md already serve this; keep them current.
4. **Governance one-pager** — how decisions are made today, what we'd adopt under a
   foundation (needed for CNCF/LF/ASF, reusable for all).

---

*Naming note: third-party organization and program names above are outreach targets,
not product language; product vocabulary rules ([README](README.md) principle 5) apply
to product and docs surfaces, not to grant applications.*
