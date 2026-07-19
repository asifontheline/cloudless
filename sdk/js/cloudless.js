// Cloudless JavaScript/TypeScript SDK — a first-class client for the community
// mesh. Zero dependencies: uses the built-in `fetch` (Node 18+ and every
// modern browser). Wraps the open Cloudless HTTP API (see PROTOCOL.md).
//
//   import { Client } from "./cloudless.js";
//   const mesh = new Client("http://localhost:8080", { apiKey: "..." });
//   console.log(await mesh.chat("Hello from JavaScript"));

export const VERSION = "0.1.0";

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

  async _request(method, path, body) {
    const headers = { "Content-Type": "application/json" };
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
  /** Send a chat request and return the assistant's text. */
  async chat(messages, model = "default", params = {}) {
    if (typeof messages === "string") messages = [{ role: "user", content: messages }];
    const out = await this._request("POST", "/v1/chat/completions", { model, messages, ...params });
    return out.choices[0].message.content;
  }

  /** Full chat-completions response (raw object). */
  async completions(messages, model = "default", params = {}) {
    if (typeof messages === "string") messages = [{ role: "user", content: messages }];
    return this._request("POST", "/v1/chat/completions", { model, messages, ...params });
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
}

export default Client;
