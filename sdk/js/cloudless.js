// Cloudless JavaScript/TypeScript SDK — a first-class client for the community
// mesh. Zero dependencies: uses the built-in `fetch` (Node 18+ and every
// modern browser). Wraps the open Cloudless HTTP API (see PROTOCOL.md).
//
//   import { Client } from "./cloudless.js";
//   const mesh = new Client("http://localhost:8080", { apiKey: "..." });
//   console.log(await mesh.chat("Hello from JavaScript"));

export const VERSION = "0.2.0";

export class CloudlessError extends Error {
  constructor(status, message) {
    super(`[${status}] ${message}`);
    this.name = "CloudlessError";
    this.status = status;
  }
}

export class Client {
  /**
   * @param {string} baseUrl  e.g. "http://localhost:8080"
   * @param {{apiKey?: string, timeoutMs?: number}} [opts]
   */
  constructor(baseUrl = "http://localhost:8080", opts = {}) {
    this.baseUrl = baseUrl.replace(/\/+$/, "");
    this.apiKey = opts.apiKey || null;
    this.timeoutMs = opts.timeoutMs ?? 60000;
  }

  async _request(method, path, body, extraHeaders) {
    const headers = { "Content-Type": "application/json", ...(extraHeaders || {}) };
    if (this.apiKey) headers["Authorization"] = "Bearer " + this.apiKey;
    const ctl = new AbortController();
    const t = setTimeout(() => ctl.abort(), this.timeoutMs);
    let resp;
    try {
      resp = await fetch(this.baseUrl + path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: ctl.signal,
      });
    } finally {
      clearTimeout(t);
    }
    const text = await resp.text();
    if (!resp.ok) throw new CloudlessError(resp.status, text);
    return text ? JSON.parse(text) : null;
  }

  // ---- AI: chat & completions ---------------------------------------------
  /** Send a chat request and return the assistant's text.
   *  `params.race = k` runs it on the k fastest healthy nodes; first
   *  complete answer wins. */
  async chat(messages, model = "default", params = {}) {
    const out = await this.completions(messages, model, params);
    return out.choices[0].message.content;
  }

  /** Full chat-completions response (raw object). */
  async completions(messages, model = "default", params = {}) {
    if (typeof messages === "string") messages = [{ role: "user", content: messages }];
    const { race, ...rest } = params;
    const headers = race ? { "X-Race": String(race) } : undefined;
    return this._request("POST", "/v1/chat/completions", { model, messages, ...rest }, headers);
  }

  /** Parallel fan-out: 1-64 independent requests spread across the mesh,
   *  results in submission order (each: {status, backend, body}). */
  async batch(requests, path = "/v1/chat/completions") {
    return (await this._request("POST", "/v1/batch", { path, requests })).results || [];
  }

  async models() {
    return (await this._request("GET", "/v1/models")).data || [];
  }

  // ---- mesh insight --------------------------------------------------------
  status() { return this._request("GET", "/status"); }
  usage() { return this._request("GET", "/usage"); }
  ledger() { return this._request("GET", "/ledger"); }
  capacity() { return this._request("GET", "/capacity"); }
  audit() { return this._request("GET", "/audit"); }

  savings(promptPer1m, completionPer1m) {
    const q = [];
    if (promptPer1m != null) q.push(`prompt_per_1m=${promptPer1m}`);
    if (completionPer1m != null) q.push(`completion_per_1m=${completionPer1m}`);
    return this._request("GET", "/savings" + (q.length ? "?" + q.join("&") : ""));
  }

  // ---- contribution / sharing ---------------------------------------------
  share() { return this._request("GET", "/share"); }
  setShare({ cpuPercent, shareWhen } = {}) {
    const body = {};
    if (cpuPercent != null) body.cpu_percent = cpuPercent;
    if (shareWhen != null) body.share_when = shareWhen;
    return this._request("PUT", "/share", body);
  }

  // ---- model commons -------------------------------------------------------
  async store() { return (await this._request("GET", "/store")).artifacts || []; }
  pull(name) { return this._request("POST", `/store/pull?name=${encodeURIComponent(name)}`, null); }

  // ---- durability & recovery ----------------------------------------------
  /** Per-object replica health and measured durability. */
  replication() { return this._request("GET", "/replication"); }
  /** Rebuild local objects from surviving replicas (admin key). */
  restore(names = []) { return this._request("POST", "/restore", { names }); }

  // ---- owner-encrypted vault ----------------------------------------------
  /** List vault objects (names and ciphertext hashes only). */
  async vault() { return (await this._request("GET", "/vault")).objects || []; }
  /** Seal bytes/string on the node before they replicate (admin key). */
  async vaultPut(name, data) {
    const headers = {};
    if (this.apiKey) headers["Authorization"] = "Bearer " + this.apiKey;
    const resp = await fetch(`${this.baseUrl}/vault/${encodeURIComponent(name)}`,
      { method: "PUT", headers, body: data });
    const text = await resp.text();
    if (!resp.ok) throw new CloudlessError(resp.status, text);
    return JSON.parse(text);
  }
  /** Open a sealed object — succeeds only on the owner's node (admin key). */
  async vaultGet(name) {
    const headers = {};
    if (this.apiKey) headers["Authorization"] = "Bearer " + this.apiKey;
    const resp = await fetch(`${this.baseUrl}/vault/${encodeURIComponent(name)}`, { headers });
    if (!resp.ok) throw new CloudlessError(resp.status, await resp.text());
    return new Uint8Array(await resp.arrayBuffer());
  }
  vaultDelete(name) { return this._request("DELETE", `/vault/${encodeURIComponent(name)}`); }

  // ---- polyglot extensions -------------------------------------------------
  /** Registered any-language extensions behind the gateway. */
  async extensions() { return (await this._request("GET", "/extensions")).extensions || []; }
  /** Call an extension: /x/<name>/<path> with your member key. */
  ext(name, path, body, method = "POST") {
    return this._request(method, `/x/${encodeURIComponent(name)}/${String(path).replace(/^\/+/, "")}`, body);
  }
}

export default Client;
