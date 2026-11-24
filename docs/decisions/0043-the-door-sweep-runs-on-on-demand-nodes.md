# 0043 — The door sweep runs on on-demand nodes, one type

**Date:** 2026-11-23
**Status:** accepted
**Arc:** next-work — [plan](../plans/2026-11-20-eks-rds-then-the-door.md), amends [0039](0039-eks-gets-rds-before-its-door-is-measured.md)
**Where it lives:** `deploy/terraform/modules/eks/main.tf` (`capacity_type`), `envs/eks/variables.tf` (`node_capacity_type`), `deploy/scripts/door-sweep.sh` (the node check)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Spot reclaimed a node every few minutes during the sweep; what do its nodes run on? | On-demand `m6a.large` for the sweep's session, chosen per apply by `node_capacity_type`; spot stays the default | Staying on spot and rerunning each run a reclaim hit | About $0.09 a cluster-hour more for two nodes; switching replaces the node group, every pod moving once |

## Context

0039 put the sweep on non-burstable spot `m6a.large`/`m6i.large`. On 2026-11-23, after
two clean runs at 10 a second (11:41–11:59 UTC), the node group recorded:

| UTC | Event |
|---|---|
| 12:11:38 | spot interruption notice: a node terminated, inside r8-3's load (12:07:15–12:12:15) |
| 12:14:16 | rebalance recommendation: a node replaced, killing r8-3's history export |
| 12:24:18 | rebalance recommendation: the node under Prometheus replaced, ending r6-1 |
| 12:13–12:26 | 10 failed spot launches, `UnfulfillableCapacity` |

A run takes about 10 minutes, so at that rate most runs lose a node, and a sweep that
needs two clean runs per speed may not finish.

## Options

**A. On-demand, one type (chosen).** No reclaims; every run on the same hardware, which
also settles which type a number belongs to.

**B. Spot, rerun what a reclaim hits.** Free per hour, but on that day's pool most
runs are thrown away and the session runs longer than A's premium buys.

**C. Spot with more instance types.** More pools, fewer reclaims; runs on mixed
hardware, which `admission-sizing.md` says a door number cannot be.

## Decision

With A–C and their costs on the table, the architect answered, 2026-11-23: "oke do it".

## Consequences

- `node_capacity_type` defaults to `SPOT`; a sweep session applies
  `-var node_capacity_type=ON_DEMAND -var 'node_instance_types=["m6a.large"]'`.
- The node group keeps its name `spot`, which `gate4b-chaos.sh` uses.
- `door-sweep.sh` prints a run whose node set changed as INVALID instead of a result,
  so a lost node can no longer pass as a slow door; it holds on either capacity type.
- The door is measured on on-demand `m6a.large`; spot `m6a.large` is the same machine,
  so the number carries to the spot default.

## Outcome

Built in `3640b3f`. The node group was replaced with on-demand `m6a.large`
on 2026-11-23. On 2026-11-24 all six sweep runs completed with no node lost and none
printed INVALID.

The replacement itself cost a stall: every pending pod went to the first node Ready
until it held its 29, and Redpanda's volume is in that node's zone, so Redpanda stayed
Pending until pods were moved by hand. A replacement that destroys before it creates
does this whenever a stateful pod's zone fills first.
