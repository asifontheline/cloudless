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

__version__ = "0.1.0"
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
    def _request(self, method: str, path: str, body: Any | None = None) -> Any:
        url = self.base_url + path
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        if self.api_key:
            req.add_header("Authorization", "Bearer " + self.api_key)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as e:
            raise CloudlessError(e.code, e.read().decode(errors="replace")) from None
        return json.loads(raw) if raw else None

    # ---- AI: chat & completions -------------------------------------------
    def chat(self, messages, model: str = "default", **params) -> str:
        """Send a chat request and return the assistant's text.

        `messages` may be a string (treated as a single user message) or a list
        of {"role", "content"} dicts.
        """
        if isinstance(messages, str):
            messages = [{"role": "user", "content": messages}]
        payload = {"model": model, "messages": messages, **params}
        out = self._request("POST", "/v1/chat/completions", payload)
        return out["choices"][0]["message"]["content"]

    def completions(self, messages, model: str = "default", **params) -> dict:
        """Full chat-completions response (raw dict)."""
        if isinstance(messages, str):
            messages = [{"role": "user", "content": messages}]
        return self._request("POST", "/v1/chat/completions", {"model": model, "messages": messages, **params})

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

    def __repr__(self) -> str:
        return f"Client(base_url={self.base_url!r})"
