# Cloudless Blueprint

## 1. Goal
Build a decentralized platform where independent systems share compute, storage, and network resources over the internet so the system can offer cloud-style services without relying on a single commercial cloud provider. AI should be part of the platform’s capabilities, but not part of the name.

This system is not just an intranet, extranet, or community network. It is a shared infrastructure platform that combines distributed compute, model hosting, orchestration, replication, and trust across community hardware.

## 1.1 Alternate Names
- Community Mesh
- Decentralized Fabric
- Edge Federation
- Peer Grid
- Distributed Commons
- Open Mesh
- Collaborative Network
- Community Cloud
- Federated Network
- Local Grid
- Mesh Platform
- Edge Intelligence Network
- Cooperative Fabric
- Autonomous Fabric

## 2. Core Principles
- Decentralized federation: no central vendor controls the system
- Open protocols: common APIs and data formats
- Resource sharing: participants contribute CPU/GPU, storage, and bandwidth
- Open-source software: avoid proprietary lock-in
- Privacy and security: encrypt data in transit and at rest
- Incentive alignment: voluntary or credit-based cooperation

## 2.1 Governing and Guiding Principles
- Manageable via web: every node embeds a web console; all administration possible from a browser
- Zero-friction setup: onboarding a node is one command with auto-detection and generated defaults; setup time is a first-class cost to minimize
- Prioritize low-cost community compute over rented infrastructure
- Keep sensitive data local and enforce privacy-first processing
- Design for community-native participation and cooperative ownership
- Replicate only what matters to stay efficient and lightweight
- Optimize workloads for heterogeneous, low-cost hardware
- Use reputation and trust to reward reliable nodes and services
- Build resilience through decentralization, redundancy, and automatic failover
- Maintain open interoperability with standard model formats and APIs
- Serve customers who value cost control, privacy, and decentralized infrastructure

## 3. Architecture

### A. Node federation
- Each participant runs a local node agent
- Nodes discover each other through:
  - Distributed hash table (DHT)
  - Bootstrap/rendezvous peers
  - Optional lightweight directory service
- Nodes advertise:
  - available CPU/GPU
  - memory
  - storage
  - network bandwidth
  - supported AI runtimes

### B. Work orchestration
- A decentralized scheduler assigns tasks to available nodes
- Tasks include:
  - model inference
  - training/fine-tuning
  - data preprocessing
- Use containerized workloads or sandboxed processes
- Lightweight orchestration alternatives:
  - Nomad / HashiCorp ecosystem
  - custom P2P scheduler
  - edge-focused orchestrators

### C. Model distribution
- Store models in a distributed content-addressed network
- Use open models in interoperable formats:
  - ONNX
  - GGUF
  - open weights from community repositories
- Nodes cache models locally to reduce repeated downloads
- Large models can be split into shards or served from nearby peers

### D. Storage and data
- Use decentralized storage for shared artifacts:
  - IPFS / libp2p file system
  - peer-to-peer object stores
- Keep private data local unless explicit permission is granted
- Share only computed outputs or sanitized summaries

### E. Network layer
- Peer-to-peer communications with:
  - TLS encryption
  - NAT traversal / hole punching
  - VPN or mesh overlay if needed
- Optimize for low bandwidth:
  - model quantization
  - delta updates
  - local caching

## 4. Governance and Agreements
- Define an agreed protocol for:
  - node onboarding
  - resource advertising
  - task acceptance
  - failure handling
- Establish trust via:
  - node reputation
  - signed identities
  - mutually agreed policies
- Use a simple credit or reputation system to prevent abuse
- Optionally use a distributed ledger or signed logs for audit

## 5. Security and Trust
- Human sign-in is passwordless by default: passkeys (WebAuthn/FIDO2) — device fingerprint/face/PIN, credential never leaves the device, phishing-resistant, with multi-device enrollment, per-device revocation, and a one-time recovery code (the cluster key remains for automation only)
- Authenticate nodes before tasks are accepted
- Run untrusted workloads in sandboxes / containers
- Encrypt all communication (membership, service, and overlay layers)
- Encrypt all data at rest on every node with a rotating key hierarchy (root → cluster → node → artifact)
- Data Guard: classify data (private/group/public, private by default), enforce locality (private data never leaves its node, group data never leaves the mesh), default-deny workload egress, audit every data movement, crypto-shred on delete
- Limit access to local data
- Accept only vetted open-source AI models in safe formats
- Monitor and revoke nodes that misbehave
- Group-held k-of-n key recovery — no single member or outsider can unlock alone

## 6. Suggested Implementation Stack
Licensing rule: only OSI-approved open-source components (Apache-2.0/MIT/BSD/MPL/GPL) — no BUSL or source-available software (excludes Nomad, Consul, Vault, ZeroTier) and no proprietary SaaS control planes (excludes Tailscale coordination).
- Networking: libp2p, WireGuard, Headscale, open mesh overlays
- Orchestration: custom P2P scheduler, Ray, Docker (Engine), Podman
- Model runtime: ONNX Runtime, PyTorch, TensorFlow Lite, llama.cpp
- Storage: IPFS, syncthing, rsync-style sync, local disk caches
- Coordination: metadata registry, DHT, small bootstrap servers
- Models: Apache-2.0/MIT open weights only as defaults (e.g. Mistral, Qwen2.5, OLMo); restricted-license weights (e.g. Meta Llama) never bundled or redistributed

## 7. Practical Phases
1. Prototype
   - Set up 2–3 trusted machines
   - Share model inference tasks
   - Use open models and local caching
2. Add discovery
   - Implement peer discovery over the internet
   - Handle NAT traversal
3. Expand
   - Add more volunteer nodes
   - Add distributed storage for models/data
   - Add task scheduling and failover
4. Governance
   - Add identity and reputation
   - Define sharing policies
   - Add credit / resource accounting

## 8. Reality Check
- This is not literally “zero cost”:
  - hardware, electricity, and network usage still matter
  - true “no cost” only exists if participants willingly donate resources
- It is possible to avoid commercial cloud fees by sharing existing machines and bandwidth
- The system will be slower and harder to manage than a centralized cloud, but it can still deliver many cloud-like AI services

## 9. Cloud Feature Replication
To make this the cheapest ever solution while still matching public cloud capabilities, the plan must replicate these cloud principles:
- Compute as a service
  - dynamic task scheduling across nodes
  - support for CPU, GPU, and accelerator workloads
- Object and model storage
  - distributed caching and content-addressed storage
  - tiered persistence: local disk, peer caches, archival nodes
- Networking and service gateway
  - API gateway for user access
  - encrypted P2P tunnels and mesh networking
- Automation and self-service
  - command / API-driven provisioning
  - node onboarding and resource discovery automation
- Monitoring and observability
  - distributed metrics collection
  - health checks, logging, and SLA-based routing
- Security and identity
  - node identity and service authentication
  - data encryption, sandboxing, and policy enforcement
- Resilience and failover
  - redundant task execution
  - automatic rerouting around unavailable nodes

## 10. Community Hardware Strategy
Community and low-cost hardware are the backbone of this plan. Key design choices:
- Treat every participant as a “community node” rather than a cloud instance
- Accept heterogeneity: different CPU types, GPUs, and memory sizes
- Use lightweight runtimes and model quantization to fit cheap hardware
- Cache models locally to reduce repeated downloads and network cost
- Implement reputation and credit so reliable nodes are preferred
- Align incentives with cooperative use, such as shared access, reputational rewards, or cost offsetting
- Add optional redundancy by assigning duplicate tasks to trusted nodes for best-in-class reliability

## 10.0 Mobile Devices as Nodes
Phones and tablets are community hardware too. The node agent must run on mobile devices with roles scaled to their limits:
- Lightweight roles first: mesh relay, blob cache, telemetry, small quantized-model inference
- Battery/network aware: contribute only while charging and on unmetered networks by default (user-controlled)
- Same security model: enrolled certificate, mutual TLS, share declarations like any node
- Delivery path: the agent cross-compiles to mobile architectures; packaged builds for phone terminals first, then native apps

## 10.1 Resource Sharing Controls and Reciprocity
Members stay in control of what they give, and what they give determines what they get:
- Each node declares share limits: CPU cores/percent, IO bandwidth, disk quota, network bandwidth, and share hours (e.g. nights only)
- The agent enforces declared limits locally; the scheduler never exceeds a node's declaration
- Contribution is metered (the ledger) and reciprocated: members earn service capacity proportional to what their nodes contribute, with community-configurable floors so small contributors are never locked out
- Changing a share declaration is a one-click console action, effective immediately

## 10.2 Community Node Tracking and the Network Map
Every node in the community mesh is tracked and visible:
- Nodes carry an optional hierarchical location label: continent / country / state / city / village
- The console renders a live network map: green dots for healthy nodes, red for down, grouped by location and expandable level by level
- The map is the community's shared operational picture — who is up, where capacity lives, where coverage is thin

## 10.3 Locality-Aware Redundancy
Failure tolerance is built around geography:
- Replicas are placed across localities: at least one copy in another city/region when the mesh spans them
- Requests prefer nearby nodes for latency; failover widens outward (village → city → state → country → continent)
- Local clusters keep serving their locality even when cut off from the wider mesh

## 11. Rule Engine Rules
The rule engine should enforce replication, monitoring, and failover for reliable services on community hardware.
- Replicate each compute task to multiple nodes when needed:
  - primary/secondary pairs for critical work
  - k-of-n redundancy for higher confidence
- Prefer nodes with higher reputation and recent success history
- Track node health with heartbeats, status updates, and health checks
- Mark nodes unhealthy after missed heartbeats or repeated failures
- Reroute failed or slow tasks automatically to alternate nodes
- Use local or nearby cached model copies to minimize network overhead
- Retry failed executions transparently and checkpoint long-running work
- Validate results before returning them for critical or non-idempotent tasks
- Use selective redundancy so high-priority jobs get stronger protection while low-priority jobs stay cheap

## 12. Example Outcome
A working “cloudless AI” system can provide:
- distributed AI inference
- collaborative model hosting
- decentralized data indexing
- edge-based AI services
- private compute on shared infrastructure

## 12. Architecture Diagram
```text
                 +---------------------------+
                 |    Client / API Layer     |
                 |  - User apps              |
                 |  - Web/mobile UI          |
                 |  - External integration   |
                 +------------+--------------+
                              |
                              v
                 +---------------------------+
                 |   Federation Gateway      |
                 |  - Request routing        |
                 |  - Task validation        |
                 |  - Policy enforcement     |
                 +------------+--------------+
                              |
          +-------------------+-------------------+
          |                                       |
          v                                       v
+----------------------+               +----------------------+
|  Node Discovery      |               |   Task Orchestration |
|  - DHT / bootstrap   |               |  - Scheduler         |
|  - Peer discovery    |               |  - Work assignment   |
|  - Node registry     |               |  - Failure retry     |
+----------+-----------+               +----------+-----------+
           |                                      |
           v                                      v
+-------------------------------------------------------------+
|                       Federated Node Mesh                    |
|  +----------------+   +----------------+   +----------------+ |
|  | Node A         |   | Node B         |   | Node C         | |
|  | - Agent        |   | - Agent        |   | - Agent        | |
|  | - Local cache  |   | - Local cache  |   | - Local cache  | |
|  | - AI runtimes  |   | - AI runtimes  |   | - AI runtimes  | |
|  | - Storage      |   | - Storage      |   | - Storage      | |
|  +-------+--------+   +-------+--------+   +-------+--------+ |
|          |                    |                    |          |
|          +-----+    +---------+----+    +----------+          |
|                |    |              |    |                     |
+-------------------------------------------------------------+
           |                    |                    |
           |                    |                    |
           v                    v                    v
+----------------+   +----------------+   +----------------+
| Model Storage  |   | Data Storage   |   | Security /     |
| - IPFS / DHT   |   | - P2P cache    |   |   Identity     |
| - Model shards |   | - Local sync   |   | - TLS / mTLS   |
+----------------+   +----------------+   +----------------+
```
