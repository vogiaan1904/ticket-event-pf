# EKS stateful tier — design

**Status:** phases (a) and (a2) built and proven; **phases (b)–(f) deferred
2026-09-19** — see *Relationship to the scope pivot*.
Phase (a)'s work order: `docs/plans/2026-09-19-eks-stateful-tier-phase-a.md`.
**Target:** the `values-eks.yaml` deploy target only. kind and k3s are unchanged.

## The problem

One `postgres` StatefulSet holds six databases: `ticketbottle_user`, `_event`,
`_payment`, `_inventory`, plus Temporal's `temporal` and `temporal_visibility`.
Four services and the workflow engine therefore share one process, one
connection limit, one disk and one failure domain.

That is the coupling the saga exists to avoid. The platform pays the full price
of distributed data — Temporal orchestration, compensation, a transactional
outbox, eventual consistency — and gets none of the isolation that price is
supposed to buy. Losing that single pod takes down every service at once, which
is the one failure a saga cannot compensate for.

Two existing artifacts already record the problem:

- `gate4b-chaos.sh` refuses to terminate a node hosting any PVC-backed pod. A
  chaos test that must route around part of the system is saying that part has
  no recovery story.
- Redpanda runs at `replicas: 1`, so the log has replication factor 1. The
  outbox guarantees payment → Kafka and marks the row published, but a single
  broker's disk loss drops events the outbox now believes were delivered. The
  pattern is correct; the deployment undercuts its guarantee.

Neither is caused by the budget. Both would still be wrong on managed services.

## Goal

EKS becomes an **ephemeral production-shape rehearsal**: created for a session,
running the real topology while up, destroyed after. The deliverable is not "we
use RDS now" — it is that Gate 4b can terminate *any* node and the purchase flow
survives, because nothing irreplaceable is left in the cluster.

## Decisions

| Decision | Call | Why |
|---|---|---|
| EKS lifecycle | Ephemeral, unchanged | No stop switch; sessions stay short |
| Postgres | **3 RDS Multi-AZ instances** | Splits the failure domain; ~$0.21/hr |
| Instance split | `payment` \| `inventory` \| `shared` | Each split has a stateable reason (below) |
| Kafka | **Redpanda in-cluster, RF=3** | MSK costs 25–40 min of session time to create and destroy |
| Redis | **Unchanged, single replica** | Holds waitroom queue state; losing it makes buyers re-queue, not lose money |
| Credentials | **Secrets Manager + Secrets Store CSI, per-service IRSA** | Keeps invariant #2: identity, not a stored secret |
| Subnet placement | **New private subnets, no NAT** | Private subnets are free; only NAT bills, and RDS needs no egress |
| Chart mechanism | **Values-gated `postgres.enabled`** | Mirrors the existing `dynamodb.enabled` precedent |

### Why these three instances

- **payment** — the money path. A shared outage here costs real money, and the
  outbox rows that record what was charged live in it.
- **inventory** — the contended one. It owns the oversell guard and the
  100-connection ceiling (`POSTGRES_MAX_OPEN_CONNS: 25` × 4 replicas). Its own
  instance makes that budget a per-instance number rather than a shared one.
- **shared** — `user`, `event`, and Temporal's two databases. Low traffic, and a
  shared outage there is recoverable.

Five instances (one per service) was considered and rejected: the two extra
instances buy isolation between `user` and `event`, which have no meaningful
contention and no independent failure story worth $0.14/hr.

## Chart changes

Approach A, following `dynamodb.enabled`. A second mechanism for the same idea
would be exactly the drift invariant #1 warns about.

**`values.yaml`**

```yaml
postgres:
  enabled: true          # false on EKS; the StatefulSet stops rendering
  hosts:                 # all three resolve to the in-cluster Service by default
    payment: postgres
    inventory: postgres
    shared: postgres
```

**`templates/infra/postgres.yaml`** — wrapped in `{{- if .Values.postgres.enabled }}`.

**`templates/apps/config.yaml`** — stops hardcoding host `postgres`; each DSN
reads `.Values.postgres.hosts.<role>`. `DATABASE_PASSWORD` and the password
inside `DATABASE_URL` leave the ConfigMap entirely.

> The password is in a **ConfigMap** today (`config.yaml:12`, `:27`, `:97`) —
> the shape `deploy/Makefile` warns about for the chart's other secrets. Moving
> to Secrets Manager fixes it as a side effect, and it must be fixed for k3s and
> kind too, where the value moves into the existing per-service Secret.

**`values-local.yaml` / `values-k3s.yaml`** — unchanged. All three hosts default
to `postgres`, so both render byte-identical to today. This is the acceptance
test for the chart phase.

**`values-eks.yaml`** — `postgres.enabled: false`, three hosts set from Terraform
outputs.

## Terraform changes

**`modules/vpc`** — **BUILT 2026-09-18.** Gained `private_subnet_cidrs`
(`10.0.11.0/24`, `10.0.12.0/24`), a private route table with **no NAT route**, and
a free S3 gateway endpoint on both route tables. Still to add for this design: an
`aws_db_subnet_group`. Additive: existing public subnets and their IDs do not
change. It lives in `envs/foundation`, so the k3s target sees the new subnets but
is not otherwise affected.

**`modules/rds`** (new) — one `aws_db_instance`, instantiated three times from
`envs/eks`:

- `engine = postgres`, `db.t4g.micro`, `multi_az = true`
- `publicly_accessible = false`, in the private DB subnet group
- security group allowing 5432 **from the node-group security group only**
- `skip_final_snapshot = true`, `backup_retention_period = 1` — ephemeral
- password from `random_password`, written to Secrets Manager

**`envs/eks`** — three `module "rds"` blocks, three Secrets Manager secrets,
three IRSA roles, plus the Secrets Store CSI driver addon.

## Credentials

Terraform generates a password per instance and stores it in Secrets Manager.
The Secrets Store CSI driver with the AWS provider mounts it into the pod; the
pod is authorised by IRSA.

**Each service's IRSA role is scoped to its own secret ARN.** `payment-service`
cannot read `inventory`'s credential. The failure-domain split is therefore
enforced at the IAM layer as well as the network layer, which is the part worth
demonstrating — it is the difference between separation and the appearance of it.

## Migrations

`migrations.yaml:29` hardcodes `-h postgres` in its `wait-postgres`
initContainer. It takes the per-service host from config instead.

RDS creates exactly one database per instance, so the `shared` instance needs
`ticketbottle_user`, `ticketbottle_event`, `temporal` and `temporal_visibility`
created explicitly. Temporal's `auto-setup` creates its own two when
`SKIP_DB_CREATE=false` and it has a superuser; the two app databases need a
bootstrap step equivalent to today's init ConfigMap.

## Redpanda RF=3

Larger than a replica-count change. The StatefulSet runs
`--mode=dev-container --smp=1`, which is explicitly a single-node mode. RF=3
means leaving dev-container mode: real seed servers, per-broker identity, an
anti-affinity rule spreading brokers across three AZs, and topic creation with
`--replicas 3`.

Kept in scope because RF=1 silently weakens a guarantee the outbox design
claims, but sequenced late — the RDS work is independently valuable and this is
the piece most likely to need iteration.

## Gate 4b

The victim guard drops its refusal of Postgres PVCs. Redis remains PVC-backed
and single-replica, so the guard keeps refusing nodes that host *it* — an
honest, narrower exclusion than today's blanket rule, and one whose reason can
be stated in a sentence.

## Phasing

Each phase leaves the tree deployable. (a) is worth landing on its own even if
the rest slips.

| # | Phase | Done when |
|---|---|---|
| a | Chart toggles + values | **DONE 2026-09-20** — overlays render byte-identical, every DSN served from a Secret, purchase-flow gate green on k3s revision 27 |
| a2 | Private subnets + `private_nodes` toggle | **DONE 2026-09-19** — Gate 3a green with `private_nodes = true`, nodes on `10.0.11.44` / `10.0.12.233` |
| b | `modules/rds` ×3 in the private subnets **— DEFERRED** | `terraform apply` in `envs/eks`; three endpoints reachable from a node |
| c | Secrets Manager + CSI + per-service IRSA **— DEFERRED** | No password in any ConfigMap or values file, on every target |
| d | Migrations against three endpoints **— DEFERRED** | All six databases exist and migrate; purchase flow completes on EKS |
| e | Redpanda RF=3 **— DEFERRED** | Topics report `--replicas 3`; a broker delete loses no event |
| f | Gate 4b guard **— DEFERRED** | Any node terminable except the Redis host; flow survives |

## Relationship to the scope pivot

This spec was written on 2026-09-16, hours after a decision to stop growing the
infra side of the project. Reconciled 2026-09-19:

- **(a2) stands.** A half-migrated node placement is worse than either end state,
  and Gate 3a is green against it.
- **(a) stands, for its credential fix.** The Postgres password is in a ConfigMap
  on *every* target including kind, where `kubectl describe` prints it in full.
  That is a defect independent of whether RDS is ever built. **Task 5 — the
  `values-eks.yaml` placeholder hosts — is out of scope**: unresolvable hostnames
  for instances nobody is building are not worth committing.
- **(b)–(f) are deferred.** Each is sound; none is the highest-value work
  available. The ranked backlog that outranks them: lift the inventory
  serialization ceiling *and measure it*, test the four NestJS services, and more
  order-flow-shaped correctness work.

Deferred is not cancelled. The problem statement above is still true: one
StatefulSet holding six databases is the coupling the saga exists to avoid.

## Success criteria

- [ ] `helm template -f values-local.yaml` and `-f values-k3s.yaml` render
      byte-identical to the current output.
- [ ] `make gate1` passes on kind with no RDS in the picture.
- [ ] On EKS, the purchase flow completes with three RDS instances behind it.
- [ ] `payment`'s RDS instance reboots with `inventory` unaffected and the queue
      still admitting.
- [ ] Gate 4b terminates a node hosting `order-service` **and** a former
      Postgres PVC-carrying workload, and the flow survives.
- [ ] No pod holds a database password sourced from a ConfigMap or a values file.
- [ ] `terraform destroy` leaves nothing billing: `eks-leak-check.sh` reports every
      line OK, including the three RDS instances and their subnet group.

## Non-goals

- **MSK.** Priced and rejected: 25–40 minutes of session time to create and
  destroy, against a Redpanda RF=3 that gets the same architectural property.
- **ElastiCache.** Redis holds recoverable queue state.
- **Always-on EKS.** The cluster stays ephemeral.
- **Per-service instances for `user` and `event`.** See "Why these three".
- **A standing NAT gateway.** Node placement is now a toggle rather than a
  non-goal: `envs/eks` takes `private_nodes`, and when it is true the NAT is
  created *in that env* so `terraform destroy` removes the meter. Default stays
  `false` (public nodes, SG-only exposure, the layout AWS documents with that
  condition attached); `true` buys AWS's recommended layout for ~$0.22/session.
  `envs/foundation` never creates one.

## Cost

| Item | Rate | 2-hour session |
|---|---|---|
| EKS control plane | $0.10/hr | $0.20 |
| 2 spot nodes | ~$0.03/hr | $0.06 |
| 3× db.t4g.micro Multi-AZ | ~$0.21/hr | $0.42 |
| ALB | ~$0.023/hr | $0.05 |
| **Total** | | **~$0.73** |

Against ~$0.31/session today. Private subnets add nothing; no NAT gateway is
created.

## Risks

- **RDS provisioning is 15–20 minutes** and Multi-AZ deletion is not instant.
  Session turnaround grows. Mitigation: provision the three in parallel, and
  keep `skip_final_snapshot = true`.
- **The VPC module is shared with k3s.** The change is additive, but it is
  applied in `envs/foundation`, so a mistake there affects a running box. Apply
  and inspect the plan with the box stopped.
- **Redpanda RF=3 on 2 spot nodes** cannot satisfy a 3-AZ anti-affinity rule.
  Either the node group grows to 3 during this work, or the rule is
  `preferredDuringScheduling` with the limitation recorded.
