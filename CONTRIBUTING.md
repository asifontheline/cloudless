# Contributing to Cloudless

Cloudless is community-owned by design. **You do not need to know Go to contribute** —
the platform is an open, language-agnostic API, and that API is the extension surface.
Build in whatever language you already use.

## Contribute in any language

| You want to… | Do it in | How |
|---|---|---|
| Use the mesh from an app | Any language | Call the standard chat-completions API at `http://<node>:8080/v1` |
| Add a new service / workload | Any language | Register with a node over the open API (subprocess) or ship a sandboxed WASM module |
| Add an inference/compute backend | Python, C++, Go, … | Implement the runtime contract; a Python worker is as valid as a Go one |
| Improve the node agent itself | Go | The core binary is Go, but this is only one of many ways in |
| Build an SDK / tooling | Any language | The API contract is public (see [PROTOCOL.md](PROTOCOL.md)); Python and JS SDKs are first-class |

Python is treated as a first-class citizen because it's the dominant language for AI/ML —
if something is awkward from Python, that's a bug we want to hear about.

## The check-in flow (how code reaches `main`)

We do **not** commit directly to `main`. Every change — from anyone, anywhere — goes
through validation and review:

1. **Branch** off `main`: `git checkout -b your-feature`
2. **Commit** your work on that branch.
3. **Open a pull request** to `main`.
4. **Validation runs automatically** (the CI workflow): build, `go vet`, `go test`, and
   `gofmt`. A red build cannot be merged.
5. **Review**: a maintainer reviews the PR. When it's approved, it gets the
   `ready-to-merge` label.
6. **Merge queue**: every 2 hours, a scheduled job merges all `ready-to-merge` PRs whose
   checks are green — squashed into `main`. Main is always green; nobody's half-finished
   work blocks anyone else's.

This means the world can push in parallel and everything lands cleanly, on a predictable
cadence, without breaking `main`.

## Ground rules (non-negotiable)

These are project principles, enforced in review:

- **Open-source licenses only** — OSI-approved (Apache-2.0/MIT/BSD/MPL) or public domain.
  No BUSL/source-available, no proprietary control planes, no restricted-license model weights.
- **Own vocabulary** — we replicate cloud *capabilities*, never cloud *branding*. No
  third-party product names in product/feature naming or UI.
- **One surface** — a feature ships its web console page, HTTP API, and CLI together.
- **Security is part of "done"** — not a later pass. Encrypt in transit and at rest;
  verify artifacts; least privilege.
- **Safe by default** — a contributed node shares 5% by default (tunable to 70%, never
  100%); nothing should ever make a machine hot or feel like a virus.
- **Tests are part of the feature** — a feature PR includes test cases covering its
  acceptance criteria; reviewers reject PRs without them. CI prints per-package coverage
  and warns when a PR lowers the total — write the tests with the code, not after.

## Getting set up

```sh
git clone https://github.com/asifontheline/cloudless
cd cloudless
git config core.hooksPath .githooks   # pre-commit: gofmt/vet/build/test — catches CI failures before they leave your machine
cd cloudless
go build ./... && go test ./...   # or just run: cloudless up
```

The open board is our roadmap: [issues](../../issues) · [milestones](../../milestones) ·
[BACKLOG.md](BACKLOG.md). Pick anything labelled `P1`, or open an issue proposing something new.

Licensed under Apache-2.0. By contributing you agree your contribution is licensed the same way.
