# Cloudless Python SDK

A first-class Python client for [Cloudless](https://cldless.com) — the community mesh
alternative to commercial cloud. **Zero dependencies** (standard library only).

## Install

```sh
pip install ./sdk/python        # from the repo
# or, once published:  pip install cloudless
```

## Use

```python
from cloudless import Client

mesh = Client("http://localhost:8080", api_key="<your key>")

# AI inference — routed and failed over across the mesh
print(mesh.chat("Explain the mesh in one line"))

# Insight
print(mesh.status())        # nodes and health
print(mesh.usage())         # tokens per key/node
print(mesh.ledger())        # who contributed vs consumed
print(mesh.savings())       # hosted-API equivalent cost

# Contribute from code
mesh.set_share(cpu_percent=40, share_when="charging")

# Model commons
print(mesh.store())         # hash-verified artifacts
mesh.pull("qwen2.5-7b.gguf")  # fetch from a peer before any public repo
```

The SDK wraps the open Cloudless API documented in
[PROTOCOL.md](https://github.com/asifontheline/cloudless/blob/main/PROTOCOL.md). Anything the
SDK does, you can do from any language with an HTTP client — Python is just first-class
because it's where most AI/ML work happens.

Apache-2.0.
