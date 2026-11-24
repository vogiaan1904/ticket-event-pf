# 0039 — EKS gets RDS before its door is measured, on non-burstable nodes

**Date:** 2026-11-20
**Status:** accepted
**Arc:** next-work — [plan](../plans/2026-11-20-eks-rds-then-the-door.md), [design](../design/eks-stateful-tier.md)
**Where it lives:** `deploy/terraform/envs/eks/`, `deploy/helm/ticketbottle/values-eks.yaml`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What does 0031's item 4, EKS's door measured, measure, and on what? | Stateful-tier phases (b)–(d) first: three RDS instances, `shared` on `db.t4g.small`; then the door, swept on non-burstable spot nodes that become EKS's default | The door on today's in-cluster Postgres and burstable `t3`/`t3a` spot nodes | About $0.45 a cluster-hour instead of $0.31; RDS adds 15–20 minutes to each session's start; phases (e)–(f) stay deferred, so a node holding Redpanda or Redis still cannot be killed |

## Context

0031 ranked EKS's door fourth and kept phases (b)–(f) behind a door above about 20 a
second. Items 1–3 are done (2026-11-18), so item 4 is next, with no plan yet.

On 2026-11-20 the architect gave the EKS target a budget of about $150. Ephemeral EKS
with three RDS instances costs about $0.45 an hour, so the budget buys about 300
cluster-hours, and cost no longer decides the order. Three facts do:

- A door measured on in-cluster Postgres sizes a topology this work then replaces.
  Temporal is that Postgres's heaviest writer (k3s, 2026-09-27).
- EKS nodes are spot `t3.large`/`t3a.large`. Both are burstable, and spot picks between
  them, so a door measured there depends on the CPU-credit balance and on which
  instance type spot picked
  (`docs/design/admission-sizing.md`, *Unlimited credits are required*).
- The design sizes `shared` (user, event, Temporal ×2) at `db.t4g.micro`, 1 GiB, and
  warns that it no longer fits once concurrent checkouts reach the thousands. At a door of
  10 a second and a 6m30s checkout lifetime ([0036](0036-a-hold-outlives-the-payment-window-by-30s.md)),
  that is about 3,900.

## Options

**A. Measure the door only.** About $1 a session. The number describes in-cluster
Postgres on burstable nodes, and would have to be measured again after any later RDS move.

**B. Phases (b)–(d), then the door.** (Chosen.) RDS ×3, Secrets Manager + CSI +
per-service IRSA, migrations against three endpoints, then the door sweep. The
number describes the shape EKS keeps.

**C. Phases (b)–(f), then the door.** Adds Redpanda RF=3 and the Gate 4b guard.
Needs a third node for 3-AZ anti-affinity and is the piece the design expects to
need the most iteration; it does not change the door.

On the nodes: non-burstable spot (`m6a.large`, `m6i.large`: 2 vCPU, 8 GiB like
`t3.large`) for a few cents an hour more, or keep `t3`/`t3a` and accept a door that
cannot be reproduced. On `shared`: `db.t4g.small` (2 GiB) for about $0.03 an hour
more Multi-AZ, or `db.t4g.micro` as designed.

## Decision

Shown the three calls with a recommendation each (scope B, non-burstable nodes for
the measurement, `shared` at `db.t4g.small`), the architect answered "all 3 your
picks", 2026-11-20.

Not put as a separate choice: the non-burstable type becomes the EKS node group's
**default**, not just an override for the measuring session, because a door measured
on one node type describes only that type.

## Consequences

- 0031's deferral of phases (b)–(d) ends; (e)–(f) stay deferred on its terms.
- Every EKS session starts 15–20 minutes later and costs about $0.45 an hour.
- The door that comes out is EKS's number in the *Current values* table of
  `admission-sizing.md`, and the RDS sizes are then revisited against it.
- `private_nodes` defaults to `true`, as `terraform.tfvars.example` already sets it.
- Holds while nodes stay non-burstable. Moving EKS to burstable or Graviton nodes needs the door measured again.

## Outcome

Built in `73c2d8c` (nodes), `29f1800` and `6b670cb` (RDS ×3), `d488675` and
`a984ba5` (Secrets Manager, CSI, per-service IRSA), and the sweep tooling in `4cd241a`.
On EKS, 2026-11-23 to 2026-11-24:

- Gate 3 passed against three RDS instances, after the clients spoke TLS ([0042](0042-eks-postgres-clients-speak-tls-unverified.md)).
- `payment-service`'s role read its own secret and was refused inventory's.
- The door was measured at **4 a second** on two `m6a.large`, and set in
  `values-eks.yaml`. 6 and 5 each failed a run. `shared` stayed under 40% average CPU
  at every speed, with at most 58 connections; the cluster saturates at about 6.4
  purchases a second. Runs:
  `docs/plans/2026-11-20-eks-rds-then-the-door.md#5-the-door-sweep`.
- Spot reclaims ended two runs; the sweep moved to on-demand ([0043](0043-the-door-sweep-runs-on-on-demand-nodes.md)).
  The non-burstable type stays the default; the capacity type does not.

`shared` at `db.t4g.small` is oversized for this door, not undersized. Not resized: the
door is far below the thousands of concurrent checkouts the design worried about.
