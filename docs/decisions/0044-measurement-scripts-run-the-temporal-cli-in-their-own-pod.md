# 0044 — Measurement scripts run the Temporal CLI in their own pod

**Date:** 2026-11-24
**Status:** proposed
**Arc:** measurement-tooling — [plan](../plans/2026-11-24-temporal-cli-outside-the-server.md)
**Where it lives:** `deploy/scripts/temporal-cli.sh` (to be written); its callers `purchase-cost.sh`, `saga-histories.sh`, `confirm-overlap.sh`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| The scripts that read saga histories run the Temporal CLI inside the server's container, and on 2026-11-24 that OOM-killed the server; where does the CLI run? | A `temporal-cli` pod from `temporalio/admin-tools`, created on first use, as `pg.sh` makes `pg-client` | Raising the server's 768Mi memory limit | One more pod while measuring, and an image pull the first time; the CLI's version is pinned apart from the server's and has to be moved with it |

## Context

`purchase-cost.sh`, `saga-histories.sh` and `confirm-overlap.sh` each run
`kubectl exec deploy/temporal -- temporal workflow list …`. The CLI then runs in the
server's container, so its memory counts against the server's limit (768Mi on EKS).
On 2026-11-24 the server was OOM-killed at 05:39:31 UTC, 10s after r4-3's load ended
(EKS door sweep, [plan](../plans/2026-11-20-eks-rds-then-the-door.md#5-the-door-sweep)).
`purchase-cost.sh` was listing that run's sagas, and the listing came back with 604 of
1224.

The scripts run only after a load ends, so no run's latency was affected. But the tool
can kill the thing it measures. Its memory also counts toward the server's own memory
reading, and a restart drops the server's history cache before the next run.

## Options

**A. A CLI pod (chosen).** `temporalio/admin-tools` at the server's version, pointed at
the `temporal` Service on 7233, which both targets have. It is created on first use and
left in place, like `pg-client`. Scripts change; the chart does not.

**B. Raise the server's memory limit.** One value. The server still shares a cgroup with
the tool, so a longer window or a bigger listing can kill it again.

**C. An admin-tools Deployment in the chart**, as Temporal's own Helm chart ships it.
That adds chart surface and a pod running on every target, all the time, for a tool
used only while measuring.

## Decision

With A–C and their costs on the table, A recommended, the architect chose "Lazy CLI pod
(Recommended)", 2026-11-24.

## Consequences

- The server's container runs only the server, so its memory and restart count describe
  the server alone.
- `temporal-cli`, like `pg-client`, is not chart-managed. On EKS it dies with the
  cluster. On k3s it stays until deleted, and holds no volume.
- The admin-tools tag names both the server version and the CLI version. Moving the
  server's image moves this tag too.
