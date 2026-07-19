# Cloudless JavaScript/TypeScript SDK

A first-class JS/TS client for [Cloudless](https://cldless.com) — the community mesh
alternative to commercial cloud. **Zero dependencies** (uses built-in `fetch`; Node 18+ and
every modern browser).

## Install

```sh
npm install cloudless        # once published
# or copy sdk/js/cloudless.js into your project
```

## Use

```js
import { Client } from "cloudless";

const mesh = new Client("http://localhost:8080", { apiKey: "<your key>" });

// AI inference — routed and failed over across the mesh
console.log(await mesh.chat("Explain the mesh in one line"));

// Insight
console.log(await mesh.status());     // nodes and health
console.log(await mesh.usage());      // tokens per key/node
console.log(await mesh.ledger());     // who contributed vs consumed
console.log(await mesh.savings());    // hosted-API equivalent cost

// Contribute from code
await mesh.setShare({ cpuPercent: 40, shareWhen: "charging" });

// Model commons
console.log(await mesh.store());
await mesh.pull("qwen2.5-7b.gguf");   // fetch from a peer before any public repo
```

TypeScript types ship in `cloudless.d.ts`. The SDK wraps the open Cloudless API
([PROTOCOL.md](https://github.com/asifontheline/cloudless/blob/main/PROTOCOL.md)) — anything it
does, you can do from any language with an HTTP client.

Apache-2.0.
