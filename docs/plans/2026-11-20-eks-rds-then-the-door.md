# EKS gets RDS, then its door is measured

**Status:** completed 2026-11-24: EKS's door is 4 a second, on RDS (*Results* under
task 5). Decided by [0039](../decisions/0039-eks-gets-rds-before-its-door-is-measured.md);
changed on the way by [0041](../decisions/0041-the-eks-api-is-gated-by-iam-not-an-ip-allowlist.md),
[0042](../decisions/0042-eks-postgres-clients-speak-tls-unverified.md) and
[0043](../decisions/0043-the-door-sweep-runs-on-on-demand-nodes.md).

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

**Done 2026-11-23:** Gate 3 passed on two `m6a.large`, and every sweep run printed
that type. The sweep's nodes are on-demand by
[0043](../decisions/0043-the-door-sweep-runs-on-on-demand-nodes.md).

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
`eks-leak-check.sh` with the RDS rows passed after the session's destroy.
On `db.t4g.small`, 2026-11-24: 181.

### 3. Credentials — phase (c)

Secrets Manager + Secrets Store CSI + one IRSA role per service, each scoped to its
own secret's ARN, as the design's *Credentials*. Three instances means three
passwords: the chart's single password splits per host on EKS, while k3s keeps one.

**Done when** `helm template -f values-k3s.yaml` is byte-identical to before, and on
EKS no ConfigMap or values file holds a password, and `payment-service`'s role is
refused `inventory`'s secret.

**Built 2026-11-21, not yet run on EKS.** The first two are render assertions in
`tests/assert-render.sh` (k3s golden unchanged; the chart's password absent from an
EKS render), each seen to fail under a mutation. The third, on EKS 2026-11-24: a pod
under `payment-service`'s ServiceAccount read its own secret's name and was refused
`ticketbottle-eks-inventory-password` with `AccessDeniedException`, "no
identity-based policy allows".
Changed from task 2: the password is Terraform's alphanumeric `random_password`, not
RDS-managed, because it goes inside a DSN (design, *Terraform changes*).

### 4. Migrations and databases — phase (d)

`shared` needs `ticketbottle_user`, `ticketbottle_event` and Temporal's two
databases created. Each migration job waits on its own host. inventory's schema
path is checked rather than assumed.

**Done when** all six databases exist and migrate, and the purchase-flow gate
passes on EKS with `postgres.enabled: false`.

**Built 2026-11-21, not yet run on EKS.** Each database-backed pod creates its own
database in an `ensure-db` init container; Temporal creates its two. inventory's
schema path is unchanged: `AutoMigrate` at boot, into a database that now exists
before it starts.

**Run on EKS 2026-11-23.** The first deploy crash-looped every client without TLS:
RDS forces it, against the design → [0042](../decisions/0042-eks-postgres-clients-speak-tls-unverified.md).
After `c289d55`, Gate 3 passed on EKS. On 2026-11-24, `shared` held `temporal`,
`temporal_visibility`, `ticketbottle_event` and `ticketbottle_user`; `payment` and
`inventory` one database each.

### 5. The door sweep

`admission-sizing.md`, *How a target's door speed is found*, on EKS: wait for
Temporal's idle, then run `deploy/scripts/door-sweep.sh` from 10 a second
downward until a speed passes twice. Record the instance type spot picked in each
run.

**Done when** EKS's row in *Current values* holds a measured number with its run,
and `shared`'s CPU and connections at that door are recorded beside it.

**Results, 2026-11-23 to 2026-11-24.** Build `sha-ad9681c`, two `m6a.large`, 40 buyers
for 5 minutes a run, no payment stalls in any run:

| Run | Nodes | Purchases a second | Checkout p99 | Under 2s | `shared` CPU avg / max, connections max |
|---|---|---|---|---|---|
| r10-1 | spot | 5.91 | 5.81s | 40.4% | 36% / 48%, 42 |
| r10-2 | spot | 6.41 | 6.29s | 40.0% | 38% / 48%, 44 |
| r6-1 | on-demand | 5.93 | 2.50s | 97.1% | 37% / 47%, 44 |
| r6-2 | on-demand | 5.91 | 2.19s | 98.4% | 38% / 46%, 46 |
| r5-1 | on-demand | 4.92 | 2.67s | 98.3% | 29% / 40%, 56 |
| r5-2 | on-demand | 4.93 | 1.64s | 99.7% | 29% / 39%, 58 |
| r4-3 | on-demand | 3.95 | 1.34s | 100% | 28% / 34%, 46 |
| r4-4 | on-demand | 3.94 | 1.24s | 100% | 28% / 34%, 56 |

**The door is 4:** the highest speed under 2s in both runs. 5 is the headroom; r5-1's
opening burst waited 1.45s for its first Temporal task against 0.15s in r5-2. The
cluster saturates at about 6.4 a second, twice k3s's 3.15, and `shared` stays under
40% average CPU, so RDS is not what bounds it. Cost per purchase at 4: nodes 0.37,
Temporal 0.09 core-seconds.

Found on the way:
- Spot reclaimed a node every few minutes on 2026-11-23, ending r8-3 and a first r6-1;
  the sweep moved to on-demand and refuses a run whose nodes changed →
  [0043](../decisions/0043-the-door-sweep-runs-on-on-demand-nodes.md).
- Replacing the node group put every pod on the first node Ready until it held its 29
  (the VPC CNI's limit for an `m6a.large`), and Redpanda's volume is in that node's
  zone, so Redpanda stayed Pending until pods were moved by hand.
- `purchase-cost.sh` runs `temporal workflow list` inside the Temporal server's
  container; on 2026-11-24 that put it over its 768Mi limit and it was OOM-killed,
  10s after r4-3's load ended. That run's history count is short; its latencies
  stand.

### 6. Close

Accept 0039 with an Outcome. Update the design's phasing table, the root
`CLAUDE.md` register (the stateful-tier row and 0031's row) and the
`deployment-architecture` skill.

**Done 2026-11-24**, with 0040–0043 accepted beside 0039 and the door set in
`values-eks.yaml`.

## Cost

About $0.45 a cluster-hour; RDS adds 15–20 minutes at create and destroy. Every
session ends with `eks-teardown.sh` and a clean `eks-leak-check.sh`.
