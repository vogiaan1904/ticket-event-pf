# Affordable AWS Deployment via a Local → k3s → EKS Ladder — Design

*Date: 2026-07-09. Status: approved for planning. Scope: deploying the existing TicketBottle V2 stack to AWS as a **learning side project** at minimum cost, with hands-on Kubernetes and a toggle-off-when-idle operating model. This design deliberately **replaces and retires** `aws/PLAN.md` + `aws/ARC.md` (a ~$733/mo production-grade, multi-region EKS plan): those files are **removed as part of this work**, and this spec becomes the single AWS plan of record (git history preserves the old plan if ever wanted).*

---

## 1. Context & motivation

The repo previously contained an AWS plan (`aws/ARC.md`, `aws/PLAN.md`) — **retired by this work** (git history preserves it). It was internally coherent but specced a **production** system: EKS + self-managed MSK + self-hosted Temporal + Aurora Serverless v2 + ElastiCache + WAF + multi-region active-passive, targeting **10,000 concurrent users, 99.9% uptime, RTO < 5 min**, honestly priced at **~$733/mo (~$590 with Savings Plans)**.

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
5. Establish **a single AWS plan of record** by retiring the superseded `aws/ARC.md`, `aws/PLAN.md`, and the orphan `aws/docker-compose.dev.yml` (the whole stale `aws/` dir), and repointing root `CLAUDE.md` at this spec.

**Non-goals (explicitly cut from `aws/PLAN.md`; documented as "future, not built")**
- Multi-region / active-passive / Route 53 failover.
- The 99.9% uptime and 10,000-concurrent-user targets and their sizing.
- Always-on operation.
- Self-managed MSK, Aurora Serverless v2, ElastiCache, WAF, ArgoCD, provisioned concurrency.
- Any change to application architecture or business logic.
- Adding automated test coverage (verification is the end-to-end purchase flow — see §7).
- Preserving the old `aws/` production docs. They are **deleted** (not kept as a "reference"); this spec is the sole AWS plan. Their production ideas (multi-region, MSK, Aurora, WAF) remain recoverable from git history if ever wanted.

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
- **Rung 1.5 — Local AWS simulation (committed, $0; added 2026-07-15):** an *additive* overlay on the **existing kind cluster** — a host-side **LocalStack** provides only the AWS-native services (**DynamoDB, Lambda, API Gateway, EventBridge**), and a `values-localstack.yaml` overlay swaps order's datastore to LocalStack DynamoDB and replaces the Phase-0 `payment-events` adapter with the real payment **Lambdas** (`services/payment-svc/lambdas/`). kind keeps running all apps + infra (Redpanda/Temporal/Postgres/Redis) unchanged; the Lambdas reach back into kind via NodePorts. Verified by **Gate 1.5** (full purchase flow through DynamoDB + the Lambda path). Adds serverless/AWS-API + cross-boundary-networking learning, $0, no duplicated infra. See [the Rung 1.5 design](2026-07-15-localstack-rung-design.md).
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

---

## Appendix A — Phase 1 on a *real* AWS account (added 2026-07-20)

**What changed since the spec was written:** a real AWS account now exists with **$200 promotional credits**, and Phase 1 (real cloud) is the next work. Rung 1 (kind) and Rung 1.5 (kind + host-LocalStack) are built and green; the Helm chart exists with `values-local` + `values-localstack`; there is **no real Terraform yet** and **no `values-k3s`**. This appendix does **not** change the architecture — it sharpens Phase 1 for real billing and records the decisions made re-analyzing the approach against the new account.

**Decision confirmed:** a **hard ~$20/mo real-spend ceiling**, with **credits treated as runway, not a burn budget**. At the projected ~$16.5/mo that is **≈12 months of runway**, i.e. effectively **$0 out-of-pocket** for the life of the learning project. Consequence: the toggle-off discipline stays a *core* part of the design (not something credits let us abandon), and EKS-as-an-everyday-environment is **rejected** — its ~$73/mo control-plane floor would consume both the budget and the credits (~2.7 months) for learning we already get from the optional Rung-3 weekend sprint.

### A.1 Compute substrate — reconfirmed

The one architectural choice still open at Phase 1 is *what runs the everyday cloud environment*. Re-evaluated against the three hard constraints (real K8s learning + ≤$20/mo + toggle-off):

| Approach | Fits $20/mo + toggle-off? | K8s learning | Verdict |
|---|---|---|---|
| **k3s on ONE stoppable EC2** | ✅ one `stop` halts all compute; data on EBS/DynamoDB survives | ✅ full: Deployments, StatefulSets, PVCs, Ingress, HPA, Helm | **Chosen (unchanged from §3/§9)** |
| ECS / Fargate | ⚠️ no clean scale-to-zero for the stateful tier; per-task billing accrues | ❌ not Kubernetes — abandons a core goal | Reject |
| EKS as the *everyday* env | ❌ ~$73/mo control-plane floor eats budget + credits | ✅✅ — but already delivered by optional Rung 3 | Reject as daily; keep as Rung 3 |
| Lightsail / plain Docker on EC2 | ✅ cheap | ❌ loses K8s + real VPC/IAM/IRSA learning | Reject |

Nothing about the real account or the credits displaces **k3s on a single stoppable EC2**.

### A.2 Sharpened 2026 cost table (us-east-1, toggle-off ≈ 5 hr/day = 150 hr/mo)

Supersedes the estimate in §8 with current pricing (notably the post-Feb-2024 public-IPv4 charge, which the original table predated):

| Line item | t3.large **on-demand** | t3.xlarge **spot** | t3.large **spot** |
|---|---|---|---|
| EC2 compute (150 hr) | $12.50 | $7.50 | $3.75 |
| EBS gp3 30GB (billed even when instance stopped) | $2.40 | $2.40 | $2.40 |
| Public IPv4, ephemeral (billed only while running) | $0.75 | $0.75 | $0.75 |
| DynamoDB on-demand (learning scale) | ~$0.50 | ~$0.50 | ~$0.50 |
| ECR storage (~3GB) | $0.30 | $0.30 | $0.30 |
| Data-transfer out (first 100GB/mo free) | ~$0 | ~$0 | ~$0 |
| **Monthly total** | **~$16.5** | **~$11.5** | **~$7.7** |
| RAM | 8GB (tight) | 16GB (comfortable) | 8GB (tight) |

All three fit the ceiling. Always-on (730 hr) is out of scope for the budget: t3.large on-demand always-on ≈ **$66/mo** — the toggle-off switch is what makes this affordable, exactly as §8 argues.

### A.3 Instance sizing — default and fallback

**Default: `t3.large` on-demand (~$16.5/mo, 8GB).** Simplest operations, no spot-interruption surprises, predictable cost with margin under $20. **Gate 2 is the decision point on RAM:** if the trimmed stack (Redpanda + no-ES Temporal + single Postgres + Redis + 8 apps + k3s overhead) does not fit 8GB comfortably, **bump to `t3.xlarge` on spot (~$11.5/mo, 16GB)** — *more RAM for less money*.

**Why spot is acceptable on a single stateful box *here* (normally it isn't):** all persistent data already lives on EBS (survives interruption) or DynamoDB (off-box), and the box is already treated as ephemeral-when-off by the toggle model. A spot interruption is therefore functionally identical to a `make stop` you didn't trigger — box down, data safe, `make start` again. The only genuine downside is an occasional "capacity unavailable" on start, an acceptable failure mode for a learning environment.

**Graviton (`t4g`) is a deferred ~20% lever, not a Phase-1 gate.** `t4g.large` on-demand toggled ≈ $14/mo, but it requires arm64 rebuilds of all 8 app images plus the infra images; the Rung-1.5 work already showed architecture friction (amd64 Lambda runtime) is non-trivial. Document as a future optimization; do not block Phase 1 on it.

### A.4 The public-IPv4 toggle-off trap (new 2026 cost reality)

Since **Feb 2024 AWS charges $0.005/hr (~$3.60/mo) for every public IPv4 address** — including the instance's auto-assigned one, and including an **Elastic IP even while the instance is stopped**. For a $20 budget with a toggle-off model this is both a real line item and a trap: a parked EIP bills 24/7 and would quietly negate the savings from stopping the instance.

**Design rule:** use the **ephemeral auto-assigned public IP** (bills only while running, ~$0.75/mo at 5 hr/day), not a parked Elastic IP. The instance's IP changes on each start; reach the box by its current public IP (surfaced by `make start`), or — optional nicety — have a start-time hook update a Route 53 A record. Never allocate an idle EIP.

### A.5 Budget guardrails on *real spend* + credit-expiry awareness

The `budget/` Terraform module (§5, applied **before any compute**) is now doubly important because **credits mask the bill**: the real risk is a forgotten billable resource that only surfaces the day credits run dry. Therefore:

- Set the AWS Budget on **actual/unblended cost** (the credit-inclusive view is misleading — it can read ~$0 while resources accrue real charges against credits). Hard email alerts at **$20/mo and $40/mo** as in §8.
- Enable **Cost Anomaly Detection** (already in §8).
- **Record the credit expiry date** (visible in Billing → Credits) in the runbook — that date is the day "effectively free" ends, and the day the toggle-off discipline goes from "good habit" to "paying real money."

### A.6 Real-account prerequisites — Phase-1 "step 0"

Things LocalStack never required and that must precede any Terraform apply:

1. **Secure the root account:** enable MFA on root, then stop using root for daily work.
2. **Create an admin identity:** an IAM admin user (or AWS Identity Center user) for everyday console/CLI; a dedicated IAM user/role for Terraform.
3. **Enable billing visibility:** turn on IAM access to Billing, activate Cost Explorer.
4. **Lock the region** to `us-east-1` (cheapest; §10) to avoid stray resources in other regions escaping the budget's attention.

### A.7 Effect on the Phase-1 build order (refines §9)

Unchanged in shape, sharpened in detail:

```
account prereqs (A.6)  →  terraform: budget FIRST (now on real/unblended spend, A.5)
  →  vpc (public subnet, NO NAT)  →  ec2-k3s (t3.large on-demand, EPHEMERAL public IP (A.4),
     instance profile for DynamoDB)  →  dynamodb  →  ecr  →  iam
  →  install k3s  →  push images to ECR  →  deploy via values-k3s.yaml
  →  Gate 2 (purchase flow green + stop/start preserves data; decide t3.large vs t3.xlarge-spot on RAM)
  →  wire make stop / make start
```

### A.8 Net

Projected **~$16.5/mo real cost**, **$0 out-of-pocket** while the $200 credits last (**≈12 months of runway**), a hard $20 guardrail on real spend, and the same learning surface and Gate 2 as the original spec. The credits are held in reserve — they simply make an already-affordable plan free for its expected lifetime, and leave headroom for the optional Rung-3 EKS weekend sprint (~$10) without touching real dollars.

---

## Appendix B — Mac-offload operating model & 3–4 month roadmap (added 2026-07-20)

**Supersedes** the relevant framing of §9 and Appendix A in light of the real driving constraint and a fixed timeline: the project is a **~3–4 month** effort (not the ~12-month runway A.8 assumed); **EKS becomes a committed weekend track** (not the *optional* Rung-3 stretch of §9 / §3 Phase 2); **LocalStack (Rung 1.5) is retired** from the go-forward path in favor of **real DynamoDB**; and **k3s-on-EC2 is promoted from an intermediate rung to the primary everyday environment**. Appendix A's cost sharpening still applies verbatim — the public-IPv4 trap (A.4), real-account prerequisites (A.6), budget-on-real-spend (A.5), and instance sizing (A.3).

### B.1 Why this appendix exists: the Mac is the real constraint

The driving motivation is **not** cost — it is **local disk/memory pressure on the development Mac** (repeated out-of-disk events). Measured 2026-07-20: the local `kind` cluster (`ticketbottle`) plus Docker was holding **~37 GB of local volumes** (+ ~2.4 GB images, + ~1.5 GB `node_modules`) against only **~16 GB free** on the root volume. Running the full stack locally is what tips the machine over.

**Resolution: move the heavy stack off the Mac.** Host k3s on a stoppable EC2, use real AWS services, and reduce the Mac to a thin client. This fixes the disk problem *and* increases the real-AWS learning surface (real DynamoDB via instance-profile IAM, real ECR, real VPC/EC2) versus the retired LocalStack simulation.

### B.2 The four-tier operating model

| Tier | What runs | Where | Footprint on Mac | Used for |
|---|---|---|---|---|
| **Inner loop** | one service (native, hot-reload) + only its datastore dep | Mac, per-service `docker-compose.dev.yml` | ~few hundred MB, **ephemeral** (`down -v` after) | fast single-service logic iteration |
| **CI** | builds the app images (§4's eight; `order` builds two binaries) → ECR; infra images are pulled from upstream, not built | GitHub Actions | none | every push; nothing builds locally |
| **Primary** | full app + trimmed infra (Redpanda / Temporal-no-ES / 1 Postgres / 1 Redis) as k8s workloads; **real DynamoDB** | k3s on a **stoppable EC2** | none (all on EBS) | weekday full-stack integration + the purchase flow |
| **Stretch** | same Helm chart, ALB + IRSA, spot nodes | **EKS**, create/`destroy` per session | none | weekend cloud-native learning |

**Mac = thin client:** VS Code, git, `kubectl`, `terraform`, `aws` CLI. No Docker footprint beyond the occasional ephemeral inner-loop dep. `node_modules` may stay for IntelliSense (immaterial once the ~40 GB of Docker is gone).

**Payment event path:** runs the **in-cluster outbox-relay** (already merged to main), *not* Lambdas. Real AWS Lambda + API Gateway for payment is an optional Phase-D add, not required.

### B.3 The three dev loops

- **Inner loop (Mac):** `docker compose -f services/<svc>/docker-compose.dev.yml up` → run the service natively with hot-reload → `down -v`. Disk-light; for single-service logic work. This is the only sanctioned local Docker use — the full stack never runs on the Mac again.
- **Integration loop (k3s-EC2):** `git push` → CI builds → ECR → `kubectl rollout restart deploy/<svc>` on the EC2 (kubeconfig points at the box; reach the app via ingress / port-forward). `make start` warms the stack in ~3–5 min (PVC data survives stop/start); `make stop` at the end.
- **Weekend loop (EKS):** `terraform apply` the `eks` env → deploy the chart via `values-eks.yaml` → learn/experiment → `terraform destroy`.

### B.3a Where cross-service & full-flow testing runs (there is intentionally *no* full-stack local compose)

Retiring the old full-stack `development/` compose does **not** remove local integration testing — testing is organized **by scope**:

| Scope | Where | Notes |
|---|---|---|
| One service's logic | Mac: its `docker-compose.dev.yml` dep + the service run natively (hot-reload) | disk-light |
| A few services interacting (e.g. `order`↔`inventory`↔`payment` gRPC chain) | Mac: run *those* services natively + their deps via the per-service composes, wired on localhost | no full stack needed; Temporal-/Kafka-dependent flows (the saga) still need the full env |
| Full end-to-end purchase flow (all apps + Temporal saga + Kafka) | **k3s-EC2 (default)**; **kind as a $0 offline fallback** | the heavy tier — off the Mac by default |

**kind is retained** as the offline full-stack fallback (`make cluster-up → gate1 → cluster-down` + prune), run **ephemerally** — the *persistent* 37 GB kind cluster was the disk villain, not full-stack testing itself. A dedicated full-stack docker-compose is **rejected**: it would not solve the disk problem (datastore volumes + images are ~15–20 GB either way, still bumping the Mac's ~16 GB free) and would reintroduce app-topology drift against the Helm chart (the duplication `CLAUDE.md` consolidated away); the gates stay k8s-defined.

### B.4 Defaults (override at planning)

- **EC2:** `t3.large` on-demand (→ `t3.xlarge` if 8 GB is tight with the full stack); ephemeral public IP (A.4); `us-east-1`.
- **EBS:** **50 GB gp3** — holds k3s + ECR-pulled images + PVC data; **no build cache** because CI builds off-box. Billed even when stopped → the dominant persistent cost; keep it lean.
- **Registry:** ECR — both k3s and EKS pull from it; images are built **only** in CI.
- **DynamoDB:** real, on-demand, instance-profile IAM (k3s) / IRSA (EKS).

### B.5 Budget reconciliation (real spend, us-east-1)

| Component | Est. monthly |
|---|---|
| k3s-EC2 primary — compute (short weekday sessions, toggled) ~$2.75 + 50 GB EBS ~$4 + IP ~$0.75 + DynamoDB ~$0.5 + ECR ~$0.3 | **~$8** |
| EKS weekends — control plane + spot nodes + ALB (2 sessions/wknd, destroy-discipline) | **~$9–11** |
| CI (GitHub Actions free tier) | **~$0** |
| **Total** | **~$17–19/mo** |

Under the $20 ceiling; **EBS size and EKS session count are the swing factors.** Over 3–4 months ≈ **$50–75 total**, fully covered by the $200 credits.

### B.6 Cost discipline — the shutdown ritual (every session)

1. `make stop` the k3s EC2 (halts all compute; EBS + data survive) **or** `terraform destroy` the EKS env.
2. Confirm in the console that nothing billable is left (no running instance, no orphan LB / EIP / NAT — see A.4 and Gate 3).
3. **Never** leave either environment up overnight; **never** run k3s-EC2 and EKS simultaneously.
4. Weekly: glance at AWS Budgets (real/unblended cost; alarms at $20 / $40 per A.5) + Cost Explorer; keep the credit-expiry date in view.

### B.7 The 3–4 month roadmap (gate-driven, ~14–16 weeks)

**Phase 0 — Foundation** *(wk 1, weekdays):* real-account prerequisites (A.6: root MFA, IAM admin, billing/Cost-Explorer access, region lock) → Terraform **budget module first** (A.5) → **CI pipeline (GitHub Actions → ECR)** → `vpc` / `ecr` / `dynamodb` / `iam`. *Scaffolding you cannot deploy without.*

**Phase A — k3s-EC2 becomes the workbench → Gate 2** *(wk 1–3):* `ec2-k3s` module + `values-k3s.yaml` + gRPC health probes → deploy from ECR → purchase flow green → **stop/start preserves data = Gate 2** → wire `make stop/start` → **delete the local kind cluster + `docker system prune`, reclaiming ~40 GB on the Mac.**

**Phase B — EKS → Gate 3** *(wk 4–6):* `eks` module + `values-eks.yaml` (ALB + IRSA + spot) → flow green on EKS → **clean `terraform destroy`, no leaked NAT / EBS / LB / EIP = Gate 3** → script the create→run→destroy ritual to ~5 min.

**Phase C — cloud-native depth** *(wk 7–11):* HPA + load-test the virtual queue / inventory under concurrency; spot-interruption recovery (kill a node, watch saga + Temporal recover); observability (Container Insights or Prometheus/Grafana tracing the purchase flow); IRSA least-privilege per service; optional Karpenter or ACM + Route53 TLS on the ALB.

**Phase D — reliability + capstone** *(wk 12–16):* game-day failure injection (pod / node / DB) verifying saga compensation + Temporal recovery + outbox redelivery; DynamoDB PITR / Postgres backup to S3; External Secrets Operator + AWS Secrets Manager (the §10 Rung-3 stretch); optional real-Lambda payment path; capstone write-up → final `destroy`.

Phases advance **when the gate is green, not on a fixed date.**

### B.8 Housekeeping the inner-loop compose files (mechanical, during Phase 0/A)

- **Add** `services/payment-svc/docker-compose.dev.yml` (Postgres + Redpanda — payment needs the outbox→relay deps; it currently has none).
- **Delete** `services/api-gateway/docker-compose.dev.yml` — the gateway has no datastore; it is a gRPC client to the other services, so a lone Postgres there is a stale copy-paste that misleads.
- **`order-svc`** keeps its LocalStack-DynamoDB compose as an *offline* inner-loop option (the deployed tiers use real DynamoDB); this is the only sanctioned remaining LocalStack use.

### B.9 Net

Same well-conceived system, same gates — but the **development host stops being the bottleneck**. The Mac drops from ~40 GB of Docker to a thin client; the heavy stack lives on a toggle-off EC2 you actually learn AWS on; CI keeps both the Mac and the EC2 lean; and EKS weekends deliver cloud-native depth. All inside ~$17–19/mo and the $200 credits.
