# Affordable AWS Deployment via a Local → k3s → EKS Ladder — Design

*Date: 2026-07-09. Status: approved for planning. Scope: deploying the existing TicketBottle V2 stack to AWS as a **learning side project** at minimum cost, with hands-on Kubernetes and a toggle-off-when-idle operating model. This design deliberately **replaces the target** of `aws/PLAN.md` + `aws/ARC.md` (a ~$733/mo production-grade, multi-region EKS plan) with a learning-right-sized one.*

---

## 1. Context & motivation

The repo already contains an AWS plan (`aws/ARC.md`, `aws/PLAN.md`). It is internally coherent but specs a **production** system: EKS + self-managed MSK + self-hosted Temporal + Aurora Serverless v2 + ElastiCache + WAF + multi-region active-passive, targeting **10,000 concurrent users, 99.9% uptime, RTO < 5 min**, honestly priced at **~$733/mo (~$590 with Savings Plans)**.

That is the wrong target for the actual use case: **one person, learning, on a budget.** The gap between "10k concurrent, multi-region" and "me, learning" is ~15–40× in cost.

Two facts drive this design:

1. **The app services are nearly free to run.** All 8 app containers (`app-gateway`, `user-svc`, `event-svc`, `payment-svc`, `inventory-svc`, `order-api`, `order-consumer`, `waitroom-svc`) are tiny (~256MB each; ~23k LOC total). Cost is dominated entirely by the **stateful heavyweight tier**: EKS control plane ($73/mo floor), self-hosted Kafka/MSK, Temporal + Elasticsearch, and 3–4 separate managed datastores each with a monthly floor. Multi-region doubles all of it.

2. **"Turn it off when not using" only works on the right primitives.** An EC2 VM can be `stop`ped (pay only disk); an EKS control plane cannot be paused (destroy/recreate only); ElastiCache and Aurora cannot be cheaply paused; DynamoDB on-demand is ~free at idle. A toggle-off learning environment therefore wants its heavy stuff on a **stoppable VM** with data **on that VM or on DynamoDB**, not on always-on managed services.

**The macro-architecture itself is sound and is NOT being changed.** Per `REVIEW.md`: high-traffic ticketing genuinely needs the virtual-queue → atomic-inventory → saga → payment shape; Temporal, the outbox pattern, and Kafka are the correct tools. This design changes only **where and how** the system runs, not **what** it is.

---

## 2. Goals / non-goals

**Goals**
1. Run the *real* stack — every distributed-systems part actually working end-to-end (virtual queue, atomic inventory, Temporal saga, Kafka events, payment outbox) — on **Kubernetes**.
2. Provide genuine **hands-on K8s** (Deployments, Services, ConfigMaps/Secrets, Ingress, StatefulSets/PVCs, HPA, Helm) plus real AWS primitives (VPC, IAM, ECR, DynamoDB, Terraform).
3. **Minimum cost with a toggle-off model**: ~$0 local, ~$10–20/mo for the everyday cloud environment, ~$10/weekend for an optional EKS sprint.
4. Produce **portable artifacts**: one Helm chart + Terraform, reused unchanged across three deploy targets.

**Non-goals (explicitly cut from `aws/PLAN.md`; documented as "future, not built")**
- Multi-region / active-passive / Route 53 failover.
- The 99.9% uptime and 10,000-concurrent-user targets and their sizing.
- Always-on operation.
- Self-managed MSK, Aurora Serverless v2, ElastiCache, WAF, ArgoCD, provisioned concurrency.
- Any change to application architecture or business logic.
- Adding automated test coverage (verification is the end-to-end purchase flow — see §7).
- Rewriting `aws/ARC.md`/`PLAN.md` (left as an aspirational "production reference"; this spec supersedes them as the *build* target and should be cross-linked, not deleted).

---

## 3. Architecture — one portable chart, three targets

The **application topology never changes.** One Helm chart is authored once; the deploy target is selected by a values overlay:

```
                     Helm chart (deploy/helm/ticketbottle/)
                                    |
         +--------------------------+--------------------------+
         |                          |                          |
   values-local.yaml          values-k3s.yaml            values-eks.yaml
    kind / k3d                 1 stoppable EC2            ephemeral EKS
    (Rung 1)                   (Rung 2)                   (Rung 3)
    $0                         ~$10–20/mo                 ~$10/weekend, $0 when destroyed
    local images               ECR images                 ECR images
    DynamoDB-local             real DynamoDB              real DynamoDB
    Traefik / NodePort         Traefik ingress            ALB ingress
    static creds               EC2 instance profile       IRSA
```

**Portability is the core value:** the manifests written for free on the laptop are the same ones that run on real AWS. Learn once, deploy three places. Moving up a rung is a values-overlay + IaC delta, not a rewrite.

---

## 4. Components & the trimmed infra tier

The 8 app services deploy as-is. Savings come from trimming the heavyweight tier to fit one cheap box. All infra runs as **StatefulSets with PVCs** (so K8s stateful workloads are part of the learning).

| Concern | Today (compose, ~23 containers) | Deployed (trimmed) | Rationale |
|---|---|---|---|
| **Kafka** | `cp-kafka` + Zookeeper | **Redpanda** single node (Kafka-API compatible) — *default* | Drops Zookeeper; ~1GB vs ~3GB. Clients (Sarama/KafkaJS) speak the Kafka protocol unchanged. |
| **Temporal** | Temporal + Postgres + **Elasticsearch** | Temporal + Postgres, **no ES** (SQL visibility) | ES alone wants ~1–2GB; SQL visibility is sufficient at learning scale. |
| **Postgres** | 4 separate containers (user, event, payment, inventory) | **1 Postgres, 4 databases** | 3 fewer containers; services get different connection strings / DB names. |
| **Redis** | 2 containers (waitroom, auth) | 1 Redis, logical DBs | Tiny either way; consolidate. |
| **Order DB** | LocalStack DynamoDB | **DynamoDB-local** (Rung 1) → **real DynamoDB** (Rung 2/3) | `order-svc` is already DynamoDB-only. Real DynamoDB is ~free at idle and teaches IAM + SDK. |
| **App services (8)** | as built | unchanged, one Deployment each | Tiny; the cheap part. |

**Chosen default:** Redpanda for the Kafka swap (lightest). Documented alternative if "learn real Kafka" is preferred later: single-broker Kafka in **KRaft mode** (also one container, no Zookeeper). Either is drop-in for the clients.

---

## 5. Repo structure

The old top-level `k8s/` was empty and was deleted in the P0 cleanup. New top-level `deploy/`:

```
deploy/
  helm/
    ticketbottle/
      Chart.yaml
      charts/              # subcharts: one per app service + infra (postgres, redis, redpanda, temporal)
      values.yaml          # shared defaults
      values-local.yaml    # kind/k3d: local images, DynamoDB-local, Traefik/NodePort, no resource limits
      values-k3s.yaml      # EC2:  ECR images, real DynamoDB, resource limits/requests, ingress host
      values-eks.yaml      # EKS:  ALB ingress, IRSA service accounts, spot node selectors
  terraform/
    modules/
      vpc/                 # minimal VPC (public subnet for the k3s box; no NAT to save ~$32/mo)
      ec2-k3s/             # EC2 + instance profile + user-data installing k3s
      dynamodb/            # orders table + GSIs
      ecr/                 # image repositories
      iam/                 # roles/policies (instance profile, later IRSA)
      budget/              # AWS Budgets alarm + Cost Anomaly Detection  <-- built first
      eks/                 # (Rung 3, optional) minimal EKS, spot nodegroup
    envs/
      k3s/                 # Rung 2 root module
      eks/                 # Rung 3 root module (optional)
  README.md                # the runbook: up / down / stop / start per rung
```

Reuse existing service `Dockerfile`s and the `development/build/` contexts for image builds.

---

## 6. Data flow & application-code impact

**Near-zero application change.** The saga, Kafka flows, outbox, and queue logic are untouched; MongoDB is already removed (DynamoDB-only). The real work is **configuration + operations**:

- **Env → K8s config.** `development/envs/.env.*` become ConfigMaps (non-secret) + Secrets (credentials, JWT, provider keys) per target.
- **DynamoDB auth via IAM, not static keys.** EC2 **instance profile** on Rung 2; **IRSA** on Rung 3. Rung 1 uses DynamoDB-local with dummy creds. (Genuine AWS learning beat; also means no long-lived keys in the cluster.)
- **Wire-compatible swaps only.** Kafka→Redpanda and 4-Postgres→1-Postgres are connection-string/endpoint changes, not code changes.
- **Health/readiness probes — known task, not a surprise.** The gRPC services may not currently expose a health endpoint. Plan to add `grpc_health_probe` sidecar/binary or TCP-level probes per service. This is called out explicitly so it is scoped into the plan.

---

## 7. Testing / acceptance gates

Verification is **behavioural, end-to-end**, not unit tests. Each rung must pass the **full purchase flow** — Waitroom → Order/Temporal → Inventory → Payment → Kafka → confirm → Waitroom slot freed (the canonical chain in the `trace-purchase-flow` skill) — **before** advancing and **before** spending money:

- **Gate 1 (free):** full purchase flow green on `kind`. **No AWS spend is authorized until this passes.**
- **Gate 2:** full purchase flow green on the k3s EC2, **and** a `stop` → `start` cycle preserves data (PVCs on EBS survive a stop/start).
- **Gate 3 (optional):** full purchase flow green on EKS, then `terraform destroy` verified to leave **no leaked billable resources** (NAT/EBS/LoadBalancers/EIP).

---

## 8. Cost controls (a first-class deliverable, not an afterthought)

- **`make stop` / `make start`** wrapping `aws ec2 stop-instances` / `start-instances` — the daily on/off switch (Rung 2). Because every datastore is a pod on the box, stopping the instance halts **all** compute at once.
- **Spot instance** for the EC2 (~70% off) with an on-demand fallback documented.
- **AWS Budgets alarm + Cost Anomaly Detection**, provisioned by Terraform **before any compute** (the `budget/` module is applied first). Hard email alerts at **$20/mo and $40/mo**. This is the single most important safeguard against a forgotten resource becoming a surprise bill.
- **No NAT Gateway** (saves ~$32/mo): the k3s box sits in a public subnet with a tight security group; egress is via its public IP.
- **`terraform destroy`** is the Rung-3 "off switch"; the runbook makes teardown a first-class, checklisted step.

### Budget summary (Rung 2 home base, us-east-1, t3.large 8GB)

| Usage pattern | EC2 | Disk + DynamoDB + ECR | **Monthly total** |
|---|---|---|---|
| **Toggle-off + spot** (~4–5 hr/day) | ~$5 | ~$6 | **~$10–12** |
| **Toggle-off, on-demand** | ~$12 | ~$6 | **~$15–20** |
| Always-on, spot | ~$18 | ~$6 | ~$24 |
| Always-on, on-demand | ~$60 | ~$6 | ~$66 |

Plus **$0/mo** for Rung 1 (local) and **~$10 per weekend** for an optional Rung-3 EKS sprint (control plane ~$5 + spot nodes ~$2 + ALB/misc ~$3), **$0 when destroyed**. Compared with the `aws/PLAN.md` ~$733/mo target, this delivers the same *learning surface* (K8s, saga, Kafka, IAM, VPC, Terraform) at roughly **2–3% of the cost**.

---

## 9. Phasing — committed vs optional

- **Phase 0 — Local K8s (committed, $0):** build/verify images → author base Helm chart + `values-local.yaml` → running on `kind` → **Gate 1** (purchase flow green). All K8s authoring de-risked for free here.
- **Phase 1 — k3s EC2 (committed, ~$10–20/mo):** Terraform `budget` first → `vpc`/`ec2-k3s`/`dynamodb`/`ecr`/`iam` → install k3s → push images to ECR → deploy via `values-k3s.yaml` → **Gate 2** (flow green + stop/start preserves data) → wire `make stop/start`.
- **Phase 2 — Ephemeral EKS (optional stretch):** Terraform `eks` (spot) → deploy same chart via `values-eks.yaml` (ALB + IRSA) → **Gate 3** (flow green, then clean destroy). Built only if/when the explicit EKS credential is wanted.

---

## 10. Defaults chosen (override any of these before/at planning)

- **Region:** `us-east-1` (cheapest). Author is likely in Vietnam (ZaloPay/PayOS/VNPay); `ap-southeast-1` is closer but ~15–20% pricier. For kubectl/SSH-driven learning, latency is irrelevant → default to cost.
- **Kafka swap:** Redpanda (default). Alternative: single-broker Kafka-KRaft.
- **Image registry:** local images on kind (no registry) → **ECR** for cloud.
- **EC2 size:** t3.large (8GB) with the trimmed stack; bump to t3.xlarge (16GB) if memory is tight.
- **IaC:** Terraform throughout.
- **Secrets:** plain K8s Secrets initially. AWS Secrets Manager + External Secrets Operator is a documented Rung-3 stretch, not built.
- **Ingress:** k3s default Traefik (Rung 1/2) → AWS Load Balancer Controller / ALB (Rung 3).

---

## 11. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Forgotten cloud resource → surprise bill | Budgets alarm + Cost Anomaly Detection applied **before** compute; `terraform destroy` runbook; no NAT. |
| Trimmed stack doesn't fit t3.large (8GB) | Redpanda + no-ES Temporal + single Postgres are chosen for exactly this; documented bump to t3.xlarge; resource requests/limits tuned in `values-k3s.yaml`. |
| gRPC services lack health endpoints | Scoped as an explicit task (§6); `grpc_health_probe` or TCP probes. |
| Redpanda behaves subtly differently from Kafka | Gate 1 runs the full flow on Redpanda locally, for free, before any spend; KRaft-Kafka fallback documented. |
| EKS teardown leaks billable resources (NAT/EBS/LB/EIP) | Gate 3 explicitly verifies a clean `destroy`; prefer no-NAT topology; checklist in runbook. |
| Committed `node_modules` in TS services is broken (see project memory) | Image builds do clean installs in the Dockerfiles; do not rely on committed `node_modules`. |

---

## 12. Open decisions deferred to the plan (not blocking)

- Umbrella Helm chart vs. per-service charts + a parent — decide during Phase 0 authoring (lean umbrella with subcharts).
- Whether Temporal uses its own Postgres DB or shares the single Postgres instance (default: same instance, separate database).
- Exact ConfigMap/Secret split per service (mechanical; derived from `development/envs/.env.*`).
- Optional domain + TLS (Route 53 + cert-manager) — a Rung-2/3 nicety, not required for the gates.

---

*Bottom line: keep the well-conceived system exactly as designed; change only its runtime target from an aspirational $733/mo production estate to a $0 → ~$15/mo → ~$10-per-weekend learning ladder that still exercises real Kubernetes, real AWS, and the full distributed-systems purchase flow.*
