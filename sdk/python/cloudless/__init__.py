"""Cloudless Python SDK — a first-class client for the community mesh.

Zero third-party dependencies (standard library only), so it installs and runs
anywhere Python does. Wraps the open Cloudless HTTP API (see PROTOCOL.md).

    from cloudless import Client
    mesh = Client("http://localhost:8080", api_key="...")
    print(mesh.chat("Hello from Python"))
"""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from typing import Any, Iterator

__version__ = "0.2.0"
__all__ = ["Client", "CloudlessError"]


class CloudlessError(RuntimeError):
    """Raised when the mesh returns an error response."""

    def __init__(self, status: int, message: str):
        self.status = status
        super().__init__(f"[{status}] {message}")


class Client:
    """A client for one Cloudless node (any node serves the whole mesh)."""

    def __init__(self, base_url: str = "http://localhost:8080", api_key: str | None = None, timeout: float = 60.0):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

    # ---- low-level ---------------------------------------------------------
    def _request(self, method: str, path: str, body: Any | None = None,
                 headers: dict[str, str] | None = None) -> Any:
        raw = self._raw(method, path, json.dumps(body).encode() if body is not None else None,
                        headers, "application/json")
        return json.loads(raw) if raw else None

    def _raw(self, method: str, path: str, data: bytes | None = None,
             headers: dict[str, str] | None = None, content_type: str | None = None) -> bytes:
        req = urllib.request.Request(self.base_url + path, data=data, method=method)
        if content_type:
            req.add_header("Content-Type", content_type)
        if self.api_key:
            req.add_header("Authorization", "Bearer " + self.api_key)
        for k, v in (headers or {}).items():
            req.add_header(k, v)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                return resp.read()
        except urllib.error.HTTPError as e:
            raise CloudlessError(e.code, e.read().decode(errors="replace")) from None

    # ---- AI: chat & completions -------------------------------------------
    def chat(self, messages, model: str = "default", race: int | None = None, **params) -> str:
        """Send a chat request and return the assistant's text.

        `messages` may be a string (treated as a single user message) or a list
        of {"role", "content"} dicts. `race=k` runs the request on the k
        fastest healthy nodes and returns the first complete answer.
        """
        out = self.completions(messages, model, race=race, **params)
        return out["choices"][0]["message"]["content"]

    def completions(self, messages, model: str = "default", race: int | None = None, **params) -> dict:
        """Full chat-completions response (raw dict)."""
        if isinstance(messages, str):
            messages = [{"role": "user", "content": messages}]
        headers = {"X-Race": str(race)} if race else None
        return self._request("POST", "/v1/chat/completions",
                             {"model": model, "messages": messages, **params}, headers)

    def batch(self, requests: list[dict], path: str = "/v1/chat/completions") -> list[dict]:
        """Parallel fan-out: 1-64 independent requests spread across the mesh,
        results in submission order (each: {status, backend, body})."""
        return self._request("POST", "/v1/batch", {"path": path, "requests": requests}).get("results", [])

    def models(self) -> list[dict]:
        return self._request("GET", "/v1/models").get("data", [])

    # ---- mesh insight ------------------------------------------------------
    def status(self) -> dict:
        return self._request("GET", "/status")

    def usage(self) -> dict:
        return self._request("GET", "/usage")

    def ledger(self) -> dict:
        return self._request("GET", "/ledger")

    def savings(self, prompt_per_1m: float | None = None, completion_per_1m: float | None = None) -> dict:
        q = []
        if prompt_per_1m is not None:
            q.append(f"prompt_per_1m={prompt_per_1m}")
        if completion_per_1m is not None:
            q.append(f"completion_per_1m={completion_per_1m}")
        path = "/savings" + ("?" + "&".join(q) if q else "")
        return self._request("GET", path)

    def capacity(self) -> dict:
        return self._request("GET", "/capacity")

    # ---- contribution / sharing -------------------------------------------
    def share(self) -> dict:
        return self._request("GET", "/share")

    def set_share(self, cpu_percent: int | None = None, share_when: str | None = None) -> dict:
        body: dict[str, Any] = {}
        if cpu_percent is not None:
            body["cpu_percent"] = cpu_percent
        if share_when is not None:
            body["share_when"] = share_when
        return self._request("PUT", "/share", body)

    # ---- model commons -----------------------------------------------------
    def store(self) -> list[dict]:
        return self._request("GET", "/store").get("artifacts", [])

    def pull(self, name: str) -> dict:
        return self._request("POST", f"/store/pull?name={name}", None)

    # ---- durability & recovery --------------------------------------------
    def replication(self) -> dict:
        """Per-object replica health and measured durability."""
        return self._request("GET", "/replication")

    def restore(self, names: list[str] | None = None) -> dict:
        """Rebuild local objects from surviving replicas (admin key).
        Every object gets an explicit outcome; irrecoverable ones are listed."""
        return self._request("POST", "/restore", {"names": names or []})

    # ---- owner-encrypted vault --------------------------------------------
    def vault(self) -> list[dict]:
        """List vault objects (names and ciphertext hashes only)."""
        return self._request("GET", "/vault").get("objects", [])

    def vault_put(self, name: str, data: bytes) -> dict:
        """Seal bytes on the node before they replicate (admin key)."""
        raw = self._raw("PUT", f"/vault/{name}", data, None, "application/octet-stream")
        return json.loads(raw)

    def vault_get(self, name: str) -> bytes:
        """Open a sealed object — succeeds only on the owner's node (admin key)."""
        return self._raw("GET", f"/vault/{name}")

    def vault_delete(self, name: str) -> None:
        self._raw("DELETE", f"/vault/{name}")

    # ---- polyglot extensions ----------------------------------------------
    def extensions(self) -> list[dict]:
        """Registered any-language extensions behind the gateway."""
        return self._request("GET", "/extensions").get("extensions", [])

    def ext(self, name: str, path: str, body: Any | None = None, method: str = "POST") -> Any:
        """Call an extension: /x/<name>/<path> with your member key."""
        return self._request(method, f"/x/{name}/{path.lstrip('/')}", body)

    def __repr__(self) -> str:
        return f"Client(base_url={self.base_url!r})"
