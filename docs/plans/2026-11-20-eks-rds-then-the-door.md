# EKS gets RDS, then its door is measured

**Status:** planned 2026-11-20. Decided by
[0039](../decisions/0039-eks-gets-rds-before-its-door-is-measured.md).

**Question it serves:** 0031's item 4, *EKS's door measured*, and the design's phases
(b)–(d).

**Design:** [`docs/design/eks-stateful-tier.md`](../design/eks-stateful-tier.md). It
owns the instance split, credentials and migrations. This plan only sequences the work
and records what changed since the design was written.

## Context

Built and checked in the code, 2026-11-20:
- Phase (a): `postgres.enabled` gates the StatefulSet; every DSN reads
  `postgres.hosts.{payment,inventory,shared}`; passwords come from a Secret built from
  the gitignored secrets file (`templates/apps/secrets.yaml`).
- Phase (a2): private subnets and the `private_nodes` toggle (`envs/eks/main.tf`).
- The chart has one `postgres.user` and one password for all three hosts.
- `templates/apps/migrations.yaml` waits on one host per job, `shared` or `payment`.
- inventory runs `POSTGRES_MAX_OPEN_CONNS: 25` per replica (`templates/apps/config.yaml`).

Changed since the design (2026-09-16):
- `shared` is `db.t4g.small`, not `db.t4g.micro` (0039).
- `inventory` is `db.t4g.small` too, after task 2's check
  ([0040](../decisions/0040-inventory-rds-is-sized-for-its-pool.md)).
- The design's "9-minute hold" is now 6m30s ([0036](../decisions/0036-a-hold-outlives-the-payment-window-by-30s.md)).

## Tasks

### 1. Node group and defaults

`envs/eks/variables.tf`: `node_instance_types` defaults to `["m6a.large", "m6i.large"]`,
`private_nodes` to `true`.

**Done when** Gate 3a is green on the new nodes, and `kubectl get nodes -L
node.kubernetes.io/instance-type` shows neither `t3` type.

### 2. `modules/rds` ×3 — phase (b)

As the design's *Terraform changes*, with `shared` at `db.t4g.small` and
`payment`/`inventory` at `db.t4g.micro`. Add the `aws_db_subnet_group`.

**Done when** `terraform apply` in `envs/eks` creates three endpoints that a node
can reach with `pg_isready`, and `eks-leak-check.sh` covers the three instances
and the subnet group.

**Check before task 3:** `SHOW max_connections` on the `inventory` instance. If
it is below 100 (25 × 4 replicas, plus headroom for migrations), stop and bring the
numbers to the architect: a smaller pool or `db.t4g.small`. Do not add RDS Proxy.

**Results, 2026-11-21.** Built in `29f1800`. On EKS, `pg_isready` from a pod
reached all three endpoints. `SHOW max_connections` on `inventory`'s
`db.t4g.micro` returned 79 (recalled; the output was not saved), about 75 usable
after Postgres's 3 superuser slots and RDS's own. Below 100, so the architect chose
`db.t4g.small` for `inventory` → [0040](../decisions/0040-inventory-rds-is-sized-for-its-pool.md).
Still to show: `max_connections` on the new class, and a clean `eks-leak-check.sh`
with the RDS rows.

### 3. Credentials — phase (c)

Secrets Manager + Secrets Store CSI + one IRSA role per service, each scoped to its
own secret's ARN, as the design's *Credentials*. Three instances means three
passwords: the chart's single password splits per host on EKS, while k3s keeps one.

**Done when** `helm template -f values-k3s.yaml` is byte-identical to before, and on
EKS no ConfigMap or values file holds a password, and `payment-service`'s role is
refused `inventory`'s secret.

### 4. Migrations and databases — phase (d)

`shared` needs `ticketbottle_user`, `ticketbottle_event` and Temporal's two
databases created. Each migration job waits on its own host. inventory's schema
path is checked rather than assumed.

**Done when** all six databases exist and migrate, and the purchase-flow gate
passes on EKS with `postgres.enabled: false`.

### 5. The door sweep

`admission-sizing.md`, *How a target's door speed is found*, on EKS: wait for
Temporal's idle, then run `deploy/scripts/door-sweep.sh` from 10 a second
downward until a speed passes twice. Record the instance type spot picked in each
run.

**Done when** EKS's row in *Current values* holds a measured number with its run,
and `shared`'s CPU and connections at that door are recorded beside it.

### 6. Close

Accept 0039 with an Outcome. Update the design's phasing table, the root
`CLAUDE.md` register (the stateful-tier row and 0031's row) and the
`deployment-architecture` skill.

## Cost

About $0.45 a cluster-hour; RDS adds 15–20 minutes at create and destroy. Every
session ends with `eks-teardown.sh` and a clean `eks-leak-check.sh`.
