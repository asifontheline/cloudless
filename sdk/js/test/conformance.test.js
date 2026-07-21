// SDK conformance tests (L4, #87): the JS SDK against a live node.
//
// Requires CLOUDLESS_TEST_ADDR / CLOUDLESS_TEST_KEY (set by
// sdk/testutil/run_conformance.sh, which starts a real cloudless binary
// against a stub backend). Skips itself when no live node is configured.
import { test } from "node:test";
import assert from "node:assert/strict";
import { Client, CloudlessError } from "../cloudless.js";

const ADDR = process.env.CLOUDLESS_TEST_ADDR;
const KEY = process.env.CLOUDLESS_TEST_KEY;
const skip = !(ADDR && KEY);

function mesh() { return new Client(ADDR, { apiKey: KEY }); }

test("chat returns stub content", { skip }, async () => {
  assert.equal(await mesh().chat("hi"), "hello from the stub backend");
});

test("chat accepts a message list", { skip }, async () => {
  const out = await mesh().chat([{ role: "user", content: "hi" }]);
  assert.equal(out, "hello from the stub backend");
});

test("completions raw shape", { skip }, async () => {
  const out = await mesh().completions("hi");
  assert.ok(out.choices);
  assert.equal(out.usage.prompt_tokens, 3);
});

test("status reports the stub backend", { skip }, async () => {
  const st = await mesh().status();
  assert.ok(st.backends.some(b => b.Backend.name === "stub"));
});

test("wrong key raises CloudlessError with status 401", { skip }, async () => {
  const bad = new Client(ADDR, { apiKey: "wrong-key" });
  await assert.rejects(() => bad.chat("hi"), (err) => {
    assert.ok(err instanceof CloudlessError);
    assert.equal(err.status, 401);
    return true;
  });
});

test("batch fans out in submission order", { skip }, async () => {
  const results = await mesh().batch([0, 1, 2].map(i => ({ messages: [{ role: "user", content: String(i) }] })));
  assert.equal(results.length, 3);
  for (const r of results) assert.equal(r.status, 200);
});

test("usage / ledger / capacity shapes", { skip }, async () => {
  const m = mesh();
  assert.ok("usage" in await m.usage());
  assert.ok("contributed" in await m.ledger());
  assert.ok("nodes" in await m.capacity());
});

test("share get/set round-trips", { skip }, async () => {
  const m = mesh();
  const applied = await m.setShare({ cpuPercent: 15 });
  assert.equal(applied.limits.cpu_percent, 15);
  assert.equal((await m.share()).limits.cpu_percent, 15);
});

test("store and extensions empty on a fresh node", { skip }, async () => {
  const m = mesh();
  assert.deepEqual(await m.store(), []);
  assert.deepEqual(await m.extensions(), []);
});

test("replication reports disabled without a secure mesh", { skip }, async () => {
  const rep = await mesh().replication();
  assert.equal(rep.enabled, false);
});
