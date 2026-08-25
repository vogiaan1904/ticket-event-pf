# Rung 3 — ephemeral EKS (BUILT; GATE 3 GREEN)

**Status (2026-08-24).** Rung 3 is complete and has been through a full create → deploy → prove →
destroy cycle against the real account. `ticketbottle-eks` was created 2026-08-22 (Kubernetes 1.36,
two spot nodes across the two foundation subnets, EBS CSI addon) and destroyed 2026-08-24.

| Piece | State |
|---|---|
| `modules/eks`, `modules/irsa-role`, `envs/eks` | ✅ built, applied, destroyed cleanly |
| `values-eks.yaml` + chart EKS surface (Ingress, ServiceAccount, `storageClass`, nullable NodePort) | ✅ built |
| `deploy/scripts/eks-bootstrap.sh`, `eks-deploy.sh` | ✅ built |
| `eks-teardown.sh`, `eks-leak-check.sh`, `eks-sweep-orphans.sh`, Makefile Rung 3 section | ✅ built |
| **Gate 3a** — purchase flow green on EKS | ✅ **passed** 2026-08-24 |
| **Gate 3b** — `terraform destroy` leaves nothing billable | ✅ **passed** — all ten checks `OK` |

**The evidence for 3a is durable and worth knowing about.** Order
`TB-GATE1-20260824-JCGVD4YX` sits in the real DynamoDB table, `COMPLETED`, created
`2026-08-24T15:44:11Z` and updated four seconds later. It survived the cluster's destruction because
DynamoDB is in `envs/foundation`, off-cluster. That single row is also **the IRSA proof**: the node
role has no DynamoDB permission at all, so nothing but the `order-service` ServiceAccount could have
written it.

**The model is a weekend sprint, not an environment.** `terraform apply` Friday, learn, `terraform
destroy` Sunday. It is *never* run simultaneously with the k3s box. EKS was explicitly **rejected as
the everyday environment** — its ~$73/mo control-plane floor would consume the entire budget for
learning that a couple of weekend sessions already deliver.

> ⚠️ **There is no stop switch.** The control plane bills $0.10/hr from `apply` to `destroy` whether
> or not a pod is running. The scripted off switch is `make -C deploy eks-down`, which runs the
> ordered teardown and then the leak check — end every session with it, not by closing the laptop.
>
> Check whether it's up before assuming anything: `aws eks list-clusters --region us-east-1`. An
> empty list is the only proof.

---

## What actually changes from Rung 2

The chart is the same. The app topology is the same. Four things change, and each one is a distinct
AWS lesson:

| Concern | Rung 2 (k3s) | Rung 3 (EKS) | The lesson |
|---|---|---|---|
| **Control plane** | k3s process on the instance you own | AWS-managed, multi-AZ, you never see the masters | managed vs self-hosted trade-off |
| **Nodes** | the same one instance | managed node group on **spot** (`t3.large`/`t3a.large`), across the 2 existing public subnets | node lifecycle, spot interruption, drain |
| **Ingress** | NodePort 30000 + SSH tunnel | **ALB** via the AWS Load Balancer Controller, real public hostname | `Ingress` → cloud LB, target groups, health checks |
| **AWS credentials** | EC2 **instance profile** (node-wide) | **IRSA** — per-ServiceAccount IAM role via OIDC | per-workload least privilege |

The last row is the real prize, and it was built as a *provable* claim rather than a config step:
the node role is deliberately granted **no DynamoDB permission at all** (`modules/eks/main.tf`, the
three managed policies are worker-node / CNI / ECR-read-only). On Rung 2 every pod on the box
inherits DynamoDB from IMDS. Here, exactly one ServiceAccount can reach the orders table — so a
green Gate 3a *is* the proof that IRSA authenticated, not a coincidence.

Three IRSA roles exist: `ebs-csi` (inside `modules/eks`), `alb-controller` and `order-service` (both
in `envs/eks/main.tf`). Only `order-service` gets DynamoDB, scoped to the table ARN and its indexes.

---

## The split that trips people up: Terraform vs cluster-side

`terraform apply` does **not** give you a deployable cluster. Two pieces live in the cluster, not in
state, and must be re-run after **every** apply:

1. the **`gp3` StorageClass** (`deploy/k8s/eks/storageclass-gp3.yaml`) — EKS's only default class is
   a legacy `gp2` pointing at the removed in-tree provisioner;
2. the **AWS Load Balancer Controller** (Helm, wearing its IRSA role).

Both are `deploy/scripts/eks-bootstrap.sh` (idempotent). Then `deploy/scripts/eks-deploy.sh`
generates the account-specific values (ECR registry, order IRSA role ARN, ALB subnets, your `/32`)
from `terraform output` so they never enter git, installs the chart, and blocks until the ALB serves.

Order for a session: `terraform apply` → `eks-bootstrap.sh` → `eks-deploy.sh` → gate → teardown.

---

## What Rung 2 already pre-wired for this

Two decisions in the built infrastructure exist *only* to make Rung 3 a delta rather than a rewrite:

1. **Two public subnets across two AZs** (`10.0.1.0/24` in AZ-a, `10.0.2.0/24` in AZ-b), even though
   the k3s box only occupies the first. EKS requires subnets in ≥2 AZs; an ALB needs ≥2 to place its
   ENIs. Both spot nodes duly landed one per AZ.
2. **`kubernetes.io/role/elb = "1"` on both subnets** — the tag the AWS Load Balancer Controller uses
   to *discover* where it may place public load balancers. Without it the controller fails with a
   confusing "no subnets found".

Also reused as-is: ECR (both rungs pull the same images), the DynamoDB table, the budget module,
the S3 state bucket, and the entire Helm chart. `envs/eks` **reads** `envs/foundation` via
`terraform_remote_state` — it never recreates the VPC.

---

## The chart delta — four switches, zero forked templates

Invariant #1 held, and it is worth checking that it stays held:

```bash
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-k3s.yaml > /tmp/k3s.yaml
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-eks.yaml > /tmp/eks.yaml
diff /tmp/k3s.yaml /tmp/eks.yaml     # ~55 lines, all four switches below
```

- `storageClass: gp3` — empty on kind/k3s, so the key is omitted entirely and they keep their default provisioner.
- `gateway.nodePort: null` — `null` (not omitted, not `0`) removes the key from merged values, the
  `{{- if .nodePort }}` guard goes false, and the Service falls back to `ClusterIP` behind the ALB.
- `ingress.enabled: true` + ALB annotations — `target-type: ip` (pod IPs direct via the VPC CNI),
  `healthcheck-path: /api` (the gateway serves under `APP_GLOBAL_PREFIX=api`; `/` would 404 every
  target into unhealthy), and `inbound-cidrs` locked to your `/32` because the stack ships dev secrets.
- `serviceAccount.order.*` — creates the SA and stamps the `eks.amazonaws.com/role-arn` annotation.

`dynamodb.enabled: false` and `order.dynamodbEndpoint: ""` are **identical to k3s**. The difference
is *where the credentials come from*, not any app config: empty env creds fall through the SDK chain
to the web-identity step instead of IMDS. This is why `config.yaml` must **omit** the AWS env vars
rather than blank them — an empty-string env var still wins over IMDS.

---

## Where the shipped code diverges from the Phase B plan

Two things were changed during execution and are correct as shipped. Don't "restore" an earlier
design that says otherwise:

| Plan says | Shipped | Why |
|---|---|---|
| `irsa-role` `policy_arns` is a `list(string)` used with `toset()` | a **map keyed by a static label** (`{ alb_controller = ... }`) | `for_each` keys must be known at plan time; an ARN that comes from a resource isn't |
| redpanda StatefulSet unchanged | gained an **`fsGroup`** | CSI-provisioned EBS volumes mount root-owned; redpanda runs non-root |

---

## Gate 3 — and why the destroy half is the hard half

1. Full purchase flow green on EKS (**Gate 3a**), via `deploy/scripts/gate1-purchase-flow.sh` with
   `GW=http://<alb-hostname>/api`.
2. `terraform destroy` leaves **no leaked billable resources** (**Gate 3b**).

Half of Gate 3 is a *cleanup* test, deliberately. The classic EKS bill-after-destroy comes from
resources Terraform never created and therefore never deletes:

- **ALBs and their security groups** created by the Load Balancer Controller in response to an
  `Ingress`. Delete the k8s `Ingress` **before** `terraform destroy`, or the ALB is orphaned and
  bills forever. This is the single most likely leak here.
- **EBS volumes** from dynamically provisioned PVCs. The `gp3` class uses `reclaimPolicy: Delete`
  specifically so `helm uninstall` takes the volumes with it — verify, don't assume.
- **NAT gateways / EIPs** if a private-subnet layout is ever introduced. The VPC has none; keep it so.
- **CloudWatch log groups** — which is why `enabled_cluster_log_types` is deliberately left unset.

**The teardown is scripted, and the order is the entire content of the script**
(`deploy/scripts/eks-teardown.sh`):

| Step | What | Why it must be here |
|---|---|---|
| 1 | `kubectl delete ingress --all`, then **poll until the ALB is gone** | The controller must still be alive to receive the delete and reconcile the ALB away. Deletion is async; destroying the cluster mid-delete orphans the ALB with nobody left who knows it should die. |
| 2 | `helm uninstall tb` | Deployments, Services, StatefulSets, ConfigMaps. |
| 3 | `kubectl delete pvc --all`, then poll for PVs → 0 | **`helm uninstall` does not delete `volumeClaimTemplate` PVCs.** Kubernetes orphans them deliberately so a re-created StatefulSet can re-adopt its data. With `reclaimPolicy: Delete`, deleting the PVC is what deletes the EBS volume. |
| 4 | `helm uninstall aws-load-balancer-controller` | Removes its admission webhook cleanly; a dangling one makes later `kubectl` calls hang. |
| 5 | `terraform destroy` | Now, and only now, everything remaining *is* in state. |

`eks-leak-check.sh` then asserts ten things and exits nonzero on any leak — clusters, node-group
instances, load balancers, target groups, cluster-tagged EBS volumes, **any** unattached volume
account-wide, Elastic IPs, NAT gateways, `k8s-*` security groups, and CloudWatch log groups. It reads
the VPC id from the **foundation** state, because `envs/eks` state is empty after a destroy.

**`eks-sweep-orphans.sh` exists because this failed once.** If credentials expire mid-session,
`kubectl` looks "down", the cluster gets destroyed with the LB controller still owning an ALB, and
the ALB, its target groups, its `k8s-*` security groups and the PVC-backed volumes are stranded with
nothing left to clean them up. The sweeper deletes exactly those — narrowly scoped to `k8s-*`-named
ELB resources and `available` volumes tagged as Kubernetes PVCs — and retries, because target groups
and security groups cannot be deleted until the ELB's listeners and ENIs finish releasing
asynchronously.

---

## Beyond Gate 3 — what the cluster is *for* now

Gate 3 proved the chart runs on managed Kubernetes. The open work is proving it **behaves**, which is
what the cluster exists to make possible:

- **Node-loss recovery** — terminate a node mid-purchase and watch the Temporal saga resume and the
  outbox redeliver. This is the first test a simpler architecture would fail, and so the best
  available evidence that the complexity bought something.
- **HPA + load test** the virtual queue and inventory under real concurrency. Two ceilings are known
  in advance and neither is fixed by adding replicas — see § scaling limits below.
- **Observability** — metrics first (metrics-server is not installed by EKS); distributed tracing is
  a later, larger piece because it means instrumenting seven services in two languages.
- Optional: ACM + Route 53 TLS on the ALB, External Secrets Operator + Secrets Manager, PITR/backups.
  **Karpenter is not worth it here** — a node autoscaler earns its keep at tens of nodes; on two it
  adds a controller and an IAM role in exchange for nothing observable.

---

## Scaling limits already known from reading the code

Two ceilings exist today. Neither is a bug, and neither is lifted by scaling:

**1 — the connection ceiling.** `templates/infra/postgres.yaml` runs the stock `postgres` image with
no `max_connections` override, i.e. the default **100**. `services/inventory-svc/config/config.go`
defaults `POSTGRES_MAX_OPEN_CONNS` to **25 per replica**. Four `inventory-service` replicas exhaust
the server on their own, and the failure lands on `user-service` / `event-service` /
`payment-service` as `FATAL: sorry, too many clients already` — services that were never under load.

**2 — the lock ceiling.** `services/inventory-svc/internal/services/reservation.go` reserves under
`SELECT … FOR UPDATE` (with `Order("id")` for consistent lock acquisition) plus a guarded
`WHERE reserved + sold + q <= total` update. This is *correct* — overselling is structurally
impossible at the database layer — and it is exactly why throughput against one hot ticket class is
bounded by lock hold time rather than replica count.

**Consequence: do not autoscale `inventory-service`.** Replicas cost 25 connections each and buy no
throughput on the contended path. Lifting ceiling 2 means changing the data model, not the
infrastructure.

**3 — every PVC-backed pod is pinned to one availability zone.** The EBS CSI driver writes node
affinity onto each PersistentVolume, because an EBS volume physically exists in one AZ.
`postgres`, `redis` and `redpanda` are single-replica StatefulSets, so a node loss relocates them
only if a replacement node appears **in the same zone**; otherwise they sit `Pending` with
`volume node affinity conflict`. `WaitForFirstConsumer` solves initial placement, not relocation.

---

## A property of the end state worth stating plainly

On Rung 3 the **stateful tier does not survive the weekend.** `reclaimPolicy: Delete` is what makes
Gate 3b passable, so Postgres / Redis / Redpanda / Temporal data is destroyed with every
`terraform destroy` and re-seeded next session. Gate 2's "stop/start preserves data" property has no
Rung 3 equivalent.

Across the whole estate only three things are durable: the **DynamoDB table**, the **ECR images**,
and the **S3 tfstate**. That is by design — it is what makes the toggle-off discipline safe rather
than nerve-racking — but it is also why the k3s box remains the everyday environment rather than a
stepping stone to be discarded.

---

## If asked to compare k3s vs EKS

Be honest about the trade, don't sell EKS:

| | k3s on one EC2 | EKS |
|---|---|---|
| Cost floor | ~$8/mo, ~$0 when stopped | ~$73/mo control plane alone, $0 only when destroyed |
| Off switch | `stop-instances`, 30s, data survives | `terraform destroy`, minutes, must rebuild state |
| HA | none — one node | multi-AZ control plane, replaceable nodes |
| Ingress | NodePort + SSH tunnel | real ALB with a public hostname |
| Credentials | node-wide instance profile | per-ServiceAccount IRSA |
| K8s API | identical (k3s is CNCF-conformant) | identical |
| Best for | daily development on a budget | proving managed-Kubernetes and cloud-native ops |

The API surface being *identical* is exactly why the ladder works: everything learned about
Deployments, StatefulSets, PVCs, Services and Helm on the $8/mo box transfers unchanged.
