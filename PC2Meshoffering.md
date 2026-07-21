# PC2 Mesh Offering Catalog

## Objective

Replicate the core families of public cloud offerings as a mesh-native, community-operated platform. The goal is not to copy vendor branding, but to provide equivalent capabilities under a clear, own-vocabulary naming system that fits the Cloudless mesh model.

## Design Principles

- Service families should be usable on community hardware, not only on centralized data centers.
- Every offering should support locality-aware placement, encryption, failover, and quota-aware use.
- Names should be simple, durable, and distinct from public cloud vendor brands.
- Each service should be composable: compute, storage, network, identity, operations, and AI can be mixed into higher-level products.

## Public Cloud Service Taxonomy and Mesh Mapping

| Public cloud service family | Common vendor examples | Mesh-native offering | Mesh capability scope |
|---|---|---|---|
| Compute, VMs, containers | AWS EC2, Azure VM, GCP Compute Engine | Mesh Compute | VM-style runtimes, container execution, autoscaling, placement by locality and hardware fit |
| Serverless functions | AWS Lambda, Azure Functions, Cloud Run | Mesh Functions | Event-driven execution, lightweight tasks, retries, and function chaining |
| Object/blob storage | S3, Azure Blob, GCS | Mesh Object Vault | Object storage, versioning, lifecycle tiers, archive, and restore |
| Block storage and snapshots | EBS, Azure Disk, Persistent Disk | Mesh Block Store | Durable disk-like volumes, snapshots, backup, and recovery |
| Databases and data services | RDS, Cosmos DB, BigQuery, Redis | Mesh Data Fabric | SQL, document, key-value, and caching services with mesh replication |
| Networking, load balancing, DNS | VPC, Load Balancer, Route 53, Azure Front Door | Mesh Transit | Overlay routing, service discovery, ingress, DNS, and edge gateways |
| CDN and edge delivery | CloudFront, Azure CDN, Cloud CDN | Mesh Edge Cache | Cache-first delivery, locality-aware content release, and edge acceleration |
| Messaging and eventing | SQS, Pub/Sub, Service Bus, Event Hubs | Mesh Queue & Event Bus | Reliable queues, topics, event streams, and workflow triggers |
| AI/ML inference and training | Bedrock, Azure AI, Vertex AI | Mesh AI Fabric | Model hosting, inference endpoints, fine-tuning, and evaluation |
| Analytics and data engineering | Snowflake, Synapse, Dataflow, BigQuery | Mesh Data Lake | Batch jobs, ETL, streaming analytics, and structured query services |
| Security, identity, and secrets | IAM, KMS, Secrets Manager, WAF | Mesh Identity & Key Mesh | Identity, signed access, secret storage, key rotation, policy enforcement |
| Observability and operations | CloudWatch, Azure Monitor, GCP Logging | Mesh Ops Console | Metrics, logs, traces, health checks, alerts, and operations dashboards |
| Developer tools and CI/CD | GitHub Actions, Azure DevOps, Cloud Build | Mesh DevOps | Builds, package distribution, release pipelines, and environment provisioning |
| Edge, IoT, and device workloads | IoT Core, Azure IoT, Edge Zones | Mesh Edge Relay | Device registration, telemetry ingestion, offline sync, and edge execution |

## Proposed Mesh Naming System

The mesh should expose services with names that feel native to the platform:

- Mesh Compute
- Mesh Functions
- Mesh Object Vault
- Mesh Block Store
- Mesh Data Fabric
- Mesh Transit
- Mesh Edge Cache
- Mesh Queue & Event Bus
- Mesh AI Fabric
- Mesh Data Lake
- Mesh Identity & Key Mesh
- Mesh Ops Console
- Mesh DevOps
- Mesh Edge Relay

## Epic Structure for Delivery

These epics are intentionally sequenced after the current mesh epics are complete. Each epic should be treated as its own product family with a dedicated backlog.

### Priority Order

1. Mesh Compute & Functions
2. Mesh Storage & Recovery
3. Mesh Data Fabric
4. Mesh Transit & Edge
5. Mesh AI Fabric
6. Mesh Queue & Integration
7. Mesh Identity & Security
8. Mesh Ops & Observability
9. Mesh DevOps
10. Mesh Data Lake & Analytics
11. Mesh Edge Relay & IoT

## Backlog Summary by Epic

- Epic P — Mesh Compute & Functions
  - Foundation runtime, sandboxed execution, function triggers
  - Resource scheduling, placement, and failover
- Epic Q — Mesh Storage & Recovery
  - Object vault, archival tiers, snapshots, and restore flows
- Epic R — Mesh Data Fabric
  - SQL/document/key-value services and replication controls
- Epic S — Mesh Transit & Edge
  - Overlay networking, discovery, ingress, and CDN-style delivery
- Epic T — Mesh AI Fabric
  - Model endpoints, evaluation pipelines, and training hooks
- Epic U — Mesh Queue & Integration
  - Topics, queues, workflow triggers, and connectors
- Epic V — Mesh Identity & Security
  - Secrets, key management, access control, and policy engine
- Epic W — Mesh Ops & Observability
  - Metrics, logs, traces, health dashboards, and alerting
- Epic X — Mesh DevOps
  - Build, release, package, and environment provisioning
- Epic Y — Mesh Data Lake & Analytics
  - ETL, stream processing, and query services
- Epic Z — Mesh Edge Relay & IoT
  - Device onboarding, telemetry, and edge execution
