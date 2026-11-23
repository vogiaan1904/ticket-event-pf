# 0040 — inventory's RDS instance is sized for its pool, not the pool for the instance

**Date:** 2026-11-21
**Status:** proposed
**Arc:** next-work — [plan](../plans/2026-11-20-eks-rds-then-the-door.md), [design](../design/eks-stateful-tier.md)
**Where it lives:** `deploy/terraform/envs/eks/main.tf` (`rds_instance_classes`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| inventory's `db.t4g.micro` allows 79 connections; 4 replicas × a pool of 25 need 100. Which gives? | `db.t4g.small` for inventory, about 190 connections, pool unchanged | A pool of 15 per replica on `db.t4g.micro` | About $0.03 a cluster-hour more (Multi-AZ); the sizes match the door only until it is measured |

## Context

The plan's check before task 3: `SHOW max_connections` on inventory's instance,
stopping below 100 (`POSTGRES_MAX_OPEN_CONNS: 25` × 4 replicas, plus headroom for
migrations). On EKS, 2026-11-21, `db.t4g.micro` returned 79 (recalled; the output was
not saved). RDS sets it from memory, `DBInstanceClassMemory / 9531392`. Postgres keeps
3 slots for superusers, which RDS's master user is not, and RDS holds one for its own
admin, so about 75 are usable: 3 replicas fit, 4 do not.

The pool of 25 is the size `Reserve`'s ceiling on one hot ticket class was measured
with (`services/inventory-svc/CLAUDE.md`, 2026-09-22). RDS Proxy was ruled out by the
plan.

## Options

**A. `db.t4g.small` for inventory.** (Chosen.) 2 GiB, about 190 connections. One line
in `envs/eks`. About $0.03 a cluster-hour more.

**B. Pool 25 → 15 per replica.** Free; 60 leaves headroom. `Reserve` throughput was
measured at 25, so it would need measuring again on EKS before the door sweep's number
could be read.

**C. Cap inventory at 3 replicas.** Free; 75 fits with no headroom, so a migration job
or a rolling update's extra pod can exhaust connections.

## Decision

Shown the three with A recommended, the architect chose "db.t4g.small (Recommended)",
2026-11-21.

## Consequences

- Of the three instances only `payment` stays on `db.t4g.micro`.
- The door sweep changes one variable, the topology, against a pool already measured.
- Holds while inventory's pool × its replica ceiling stays under about 190. Raising
  either past that needs this record revisited, still without RDS Proxy.
- 0039's cost estimate rises by about $0.03 an hour.
