# Cloud Service Coverage Matrix

What the ~100 flagship public-cloud services are (AWS / Azure / GCP), and which of them Cloudless — a trusted-group mesh of community hardware — can realistically deliver.

**Verdict legend:**
- ✅ **Now/near** — deliverable with the current MVP direction (M0–M4) or a small extension
- 🟡 **Later** — feasible on the mesh architecture, needs dedicated milestones
- 🔶 **Degraded** — deliverable in a limited form (works for small groups, not at cloud scale/SLA)
- ❌ **No** — fundamentally requires centralized scale, global infrastructure, or proprietary tech

## 1. Compute (12)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 1 | Virtual machines | EC2 / Virtual Machines / Compute Engine | 🟡 | Sandboxed containers (Podman) on peer nodes; true VMs later via KVM/Firecracker on capable hosts |
| 2 | Containers as a service | ECS-Fargate / Container Apps / Cloud Run | 🟡 | Scheduler dispatches OCI containers to nodes — natural M5+ extension of the task orchestrator |
| 3 | Kubernetes | EKS / AKS / GKE | ❌ | Running managed K8s control planes contradicts the lightweight-mesh design; not worth replicating |
| 4 | Serverless functions | Lambda / Functions / Cloud Functions | 🟡 | Short sandboxed jobs routed like inference requests; queue + retry already in the rule engine |
| 5 | Batch computing | Batch / Batch / Batch | ✅ | Embarrassingly parallel jobs are the mesh's sweet spot (render, transcode, data prep) |
| 6 | Spot/preemptible capacity | Spot / Spot VMs / Preemptible | ✅ | The whole mesh *is* spot capacity — churn-tolerance is built into the rule engine |
| 7 | Auto-scaling | ASG / VMSS / MIG | 🔶 | "Scale" = recruit more volunteer nodes; elastic within the group's hardware, not infinite |
| 8 | Edge compute | Outposts-Wavelength / Stack Edge / Distributed Cloud | ✅ | Community nodes *are* edge compute — this is a native strength, not a replica |
| 9 | VPS/simple hosting | Lightsail / App Service / App Engine | 🔶 | Host small apps on trusted nodes; no SLA-backed uptime for public-facing production |
| 10 | GPU compute | EC2 P5 / NC-series / A3 | 🔶 | Consumer GPUs (RTX-class) pooled for inference/fine-tune; no H100-cluster training runs |
| 11 | HPC | ParallelCluster / CycleCloud / — | ❌ | Needs low-latency interconnects (InfiniBand); internet-linked peers can't deliver |
| 12 | Mainframe/VMware migration | — / — / — | ❌ | Out of scope entirely |

## 2. Storage (10)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 13 | Object storage | S3 / Blob / GCS | 🟡 | Content-addressed replicated store across peers (M2 model cache generalizes to this) |
| 14 | Block storage | EBS / Managed Disks / PD | ❌ | Low-latency block devices over WAN peers aren't viable |
| 15 | Shared file systems | EFS / Files / Filestore | 🔶 | Syncthing-style replication for shared folders; not POSIX-over-network at scale |
| 16 | Archive/cold storage | Glacier / Archive / Coldline | ✅ | Replicated cold blobs on high-capacity volunteer disks — cheap disks are the community's advantage |
| 17 | Backup service | AWS Backup / Azure Backup / — | ✅ | k-of-n replicated encrypted backups across trusted peers |
| 18 | CDN | CloudFront / Front Door / Cloud CDN | 🔶 | Peer caching helps a small group; global anycast CDN performance impossible |
| 19 | Data transfer appliances | Snowball / Data Box / Transfer Appliance | ❌ | N/A — no logistics arm |
| 20 | Storage gateway/hybrid | Storage Gateway / — / — | 🟡 | Node agent can expose mesh storage as local S3-compatible endpoint |
| 21 | Model/artifact registry | ECR-ish / ACR / Artifact Registry | ✅ | M2 pull-through GGUF cache *is* this; extend to OCI images later |
| 22 | Static site hosting | S3+CF / Static Web Apps / Firebase Hosting | 🔶 | Serve from replicated peers behind the gateway; fine for internal/group sites |

## 3. Databases (10)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 23 | Managed SQL | RDS / SQL Database / Cloud SQL | 🔶 | Run Postgres on a node with replica on a peer (streaming replication); group-scale only |
| 24 | Distributed SQL | Aurora / — / Spanner-AlloyDB | ❌ | Global consensus DBs need datacenter networks |
| 25 | Key-value/NoSQL | DynamoDB / Cosmos DB / Firestore | 🟡 | Replicated KV over the mesh (eventual consistency) fits the gossip architecture |
| 26 | In-memory cache | ElastiCache / Cache for Redis / Memorystore | 🔶 | Valkey (BSD) per-node; mesh-wide cache coherence not attempted |
| 27 | Document DB | DocumentDB / Cosmos-Mongo / — | 🔶 | FerretDB (Apache-2.0) on nodes; same replica story as SQL |
| 28 | Graph DB | Neptune / Cosmos-Gremlin / — | 🔶 | Single-node deployments with backup; niche |
| 29 | Time-series DB | Timestream / Data Explorer / — | ✅ | Mesh telemetry already needs one; VictoriaMetrics (Apache-2.0) per node |
| 30 | Vector DB | OpenSearch-vec / AI Search / Vertex Vector | ✅ | Qdrant (Apache-2.0) on nodes behind the gateway — pairs perfectly with LLM inference |
| 31 | Data warehouse | Redshift / Synapse / BigQuery | ❌ | Petabyte MPP warehousing needs colocated clusters |
| 32 | DB migration service | DMS / DMS / DMS | ❌ | Product-shaped service, not infrastructure — skip |

## 4. AI / ML (12)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 33 | LLM inference API | Bedrock / Azure OpenAI / Vertex-Gemini | ✅ | **The MVP.** OpenAI-compatible gateway over open-weight models |
| 34 | Embeddings API | Bedrock / OpenAI-embeddings / Vertex | ✅ | Same gateway, `/v1/embeddings`, small models run anywhere |
| 35 | Model hosting/serving | SageMaker endpoints / ML endpoints / Vertex | ✅ | Nodes serve any GGUF/ONNX model; registry + routing already designed |
| 36 | Fine-tuning | SageMaker / Azure ML / Vertex | 🟡 | LoRA fine-tunes on single consumer GPUs are feasible; distributed pretraining is not |
| 37 | Speech-to-text | Transcribe / AI Speech / Speech-to-Text | ✅ | whisper.cpp (MIT) as another runtime behind the same gateway |
| 38 | Text-to-speech | Polly / AI Speech / TTS | ✅ | Piper (MIT/OSS) on nodes |
| 39 | Translation | Translate / Translator / Translation | ✅ | Open NMT/LLM models via the gateway |
| 40 | Image generation | Bedrock-SD / OpenAI-DALLE / Imagen | ✅ | Stable Diffusion (OpenRAIL — verify against licensing policy) / FLUX-schnell on GPU nodes |
| 41 | Vision/OCR | Rekognition / AI Vision / Vision API | ✅ | Open VLMs (Qwen-VL) + Tesseract/PaddleOCR |
| 42 | ML pipelines/AutoML | SageMaker Pipelines / ML / Vertex Pipelines | 🟡 | Batch scheduler + DAGs later; AutoML skipped |
| 43 | Frontier-model access | GPT/Claude/Gemini via cloud | ❌ | By definition — Cloudless serves open weights only |
| 44 | ML feature store | SageMaker FS / — / Vertex FS | ❌ | Enterprise MLOps veneer; skip |

## 5. Networking (8)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 45 | Private networks | VPC / VNet / VPC | ✅ | WireGuard mesh overlay (M5) — arguably simpler than cloud VPCs |
| 46 | Load balancing | ELB / Load Balancer / CLB | ✅ | The gateway's ranked routing *is* an L7 balancer |
| 47 | DNS | Route 53 / Azure DNS / Cloud DNS | 🔶 | Internal mesh naming yes; public authoritative DNS no |
| 48 | API gateway | API Gateway / API Management / Apigee | ✅ | Already built — auth, routing, policy at the gateway |
| 49 | VPN access | Client VPN / VPN Gateway / Cloud VPN | ✅ | WireGuard join = the VPN |
| 50 | Dedicated links | Direct Connect / ExpressRoute / Interconnect | ❌ | Physical telco product |
| 51 | Service mesh | App Mesh / — / Traffic Director | 🔶 | mTLS + discovery covers the useful subset for group scale |
| 52 | NAT/egress management | NAT GW / NAT / Cloud NAT | 🟡 | Exit-node routing through volunteer peers (Tor-like, group-private) |

## 6. Analytics & Streaming (8)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 53 | Stream ingestion | Kinesis / Event Hubs / Pub/Sub | 🔶 | NATS (Apache-2.0) on the mesh; group throughput, not GB/s firehoses |
| 54 | Message queues | SQS / Service Bus / Cloud Tasks | ✅ | NATS/JetStream embedded in the agent — also needed internally for jobs |
| 55 | Event bus | EventBridge / Event Grid / Eventarc | 🟡 | Same substrate, routing rules on top |
| 56 | ETL/data integration | Glue / Data Factory / Dataflow | 🟡 | Batch jobs + queues compose into ETL; no visual designer |
| 57 | Big-data clusters | EMR / HDInsight / Dataproc | ❌ | Spark-scale shuffles need datacenter bandwidth |
| 58 | Ad-hoc SQL on files | Athena / Synapse serverless / BigQuery | 🔶 | DuckDB (MIT) jobs over mesh-stored Parquet — surprisingly capable for group data |
| 59 | BI dashboards | QuickSight / Power BI / Looker | 🔶 | Host Metabase/Superset (both OSS) as a mesh app |
| 60 | Search | OpenSearch / AI Search / — | 🔶 | Meilisearch (MIT) or OpenSearch on capable nodes |

## 7. Security & Identity (9)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 61 | IAM | IAM / Entra ID / Cloud IAM | 🟡 | API keys now; per-user identity + roles in the web console (M3+) |
| 62 | Key management | KMS / Key Vault / Cloud KMS | 🟡 | Cluster CA + age/OpenSSL-based secret store; no HSMs |
| 63 | Secrets manager | Secrets Manager / Key Vault / Secret Manager | ✅ | Encrypted replicated secrets, unlocked by cluster key |
| 64 | Certificates | ACM / — / Certificate Manager | ✅ | Internal CA already planned (M1b); Let's Encrypt for public edges |
| 65 | WAF/DDoS | WAF-Shield / Front Door WAF / Armor | ❌ | Meaningful DDoS absorption requires provider-scale bandwidth |
| 66 | Threat detection | GuardDuty / Defender / SCC | 🔶 | CrowdSec/Suricata (OSS) feeds into mesh monitoring; not ML-driven at cloud depth |
| 67 | Audit logging | CloudTrail / Monitor logs / Audit Logs | ✅ | Signed append-only action log — already in the blueprint (§4) |
| 68 | Compliance programs | Artifact / Compliance Manager / — | ❌ | Certifications (SOC2/HIPAA) are organizational, not software |
| 69 | Private compute/enclaves | Nitro Enclaves / Confidential VMs / Confidential | 🔶 | Where hardware supports it (SEV/TDX); bonus, not baseline |

## 8. Developer Tools & Ops (10)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 70 | Monitoring/metrics | CloudWatch / Monitor / Cloud Monitoring | ✅ | Built into the agent + web console; Prometheus-format export |
| 71 | Centralized logging | CloudWatch Logs / Log Analytics / Logging | ✅ | Loki-style (check license: Loki is AGPL — use VictoriaLogs, Apache-2.0) |
| 72 | Tracing | X-Ray / App Insights / Trace | 🟡 | OpenTelemetry (Apache-2.0) through the gateway |
| 73 | Infra as code | CloudFormation / ARM-Bicep / Deployment Mgr | 🟡 | Declarative mesh config applied via the console/API (OpenTofu-style, MPL) |
| 74 | CI/CD | CodePipeline / Azure DevOps / Cloud Build | 🔶 | Woodpecker CI (Apache-2.0) running builds as mesh batch jobs |
| 75 | Git hosting | CodeCommit / Repos / — | 🔶 | Forgejo (MIT) as a hosted mesh app |
| 76 | Container registry | ECR / ACR / Artifact Registry | ✅ | Content-addressed blob store serves OCI images too |
| 77 | Cost management | Cost Explorer / Cost Mgmt / Billing | ✅ | Usage accounting (M3) + per-node contribution ledger — the credit-system seed |
| 78 | Status/health dashboard | Health Dashboard / Status / Status | ✅ | `/ui` console already does this |
| 79 | CLI/SDK/API surface | AWS CLI / az / gcloud | ✅ | `cloudless` CLI + one HTTP API + web console (product principle) |

## 9. Application Services (8)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 80 | Email sending | SES / ACS Email / — | ❌ | Residential IPs are blacklisted; deliverability requires provider reputation |
| 81 | SMS/notifications | SNS / ACS / Firebase FCM | ❌ | Carrier relationships required |
| 82 | Workflow orchestration | Step Functions / Logic Apps / Workflows | 🟡 | DAG runner over the job queue |
| 83 | Cron/scheduled jobs | EventBridge Scheduler / Logic Apps / Scheduler | ✅ | Trivial on the scheduler; survives node failure via failover |
| 84 | Web app hosting | Amplify / Static Web Apps / Firebase | 🔶 | Group-internal apps yes; global consumer apps no |
| 85 | Auth as a service | Cognito / Entra B2C / Identity Platform | 🔶 | Self-host Keycloak/Zitadel? (check licenses; Keycloak Apache-2.0) for group apps |
| 86 | Realtime sync/push | AppSync / SignalR / Firebase RTDB | 🔶 | NATS WebSockets for group apps |
| 87 | Desktop/VDI | WorkSpaces / AVD / — | ❌ | Latency + licensing (Windows) both fail |

## 10. Specialized (13)

| # | Service | AWS / Azure / GCP | Verdict | How on Cloudless |
|---|---|---|---|---|
| 88 | IoT ingestion | IoT Core / IoT Hub / — | ✅ | MQTT broker (Eclipse Mosquitto, EPL) on nodes — LAN-local IoT is a natural fit |
| 89 | Blockchain | Managed Blockchain / — / — | ❌ | Deliberately out (see licensing/ethos; also niche) |
| 90 | Quantum | Braket / Quantum / — | ❌ | Obviously not |
| 91 | Satellite ground stations | Ground Station / Orbital / — | ❌ | Obviously not |
| 92 | Robotics | RoboMaker / — / — | ❌ | Discontinued even at AWS |
| 93 | Game servers | GameLift / PlayFab / Agones-GKE | 🔶 | Group game servers on peers work great; matchmaking at scale doesn't |
| 94 | Media transcode | MediaConvert / Media Services / Transcoder | ✅ | FFmpeg (LGPL/GPL) batch jobs — classic distributed workload |
| 95 | Live streaming | IVS / Media Services / — | 🔶 | OwnCast-style for groups; not Twitch scale |
| 96 | Maps/geo | Location / Azure Maps / Maps | ❌ | Data licensing (use OSM directly instead) |
| 97 | Healthcare/genomics APIs | HealthLake / Health Data / Healthcare API | ❌ | Compliance-bound verticals |
| 98 | Contact center | Connect / CC / CCAI | ❌ | Telephony + vertical SaaS |
| 99 | Digital twins | TwinMaker / Digital Twins / — | ❌ | Vertical SaaS |
| 100 | Disaster recovery | Elastic DR / Site Recovery / — | ✅ | k-of-n replication + failover is the core rule engine — DR is the mesh's native behavior |

## Summary

| Verdict | Count | Reading |
|---|---|---|
| ✅ Now/near | 28 | The mesh's natural territory: AI serving, batch/spot compute, backup/archive, queues, monitoring, edge |
| 🟡 Later | 16 | Feasible on this architecture with dedicated milestones |
| 🔶 Degraded (group-scale) | 25 | Works for a trusted group; don't claim cloud SLAs |
| ❌ No | 31 | Needs centralized scale, physical infrastructure, or provider reputation |

**The honest positioning for the paper:** Cloudless can credibly deliver ~44% of the cloud catalog outright or with work, and another ~25% in group-scale form — concentrated exactly where the target users' spend and privacy pain is (AI inference, batch compute, storage/backup, internal apps). What it can never replicate (global CDNs, petabyte warehouses, email deliverability, DDoS absorption, compliance certifications) is precisely the part small teams rarely need. Lead with the ✅ column; be explicit about the ❌ column — it makes the claim believable.
