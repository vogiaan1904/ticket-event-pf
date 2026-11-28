# The Temporal CLI runs outside the server

**Status:** built 2026-11-26, not yet verified on k3s. Decided by
[0044](../decisions/0044-measurement-scripts-run-the-temporal-cli-in-their-own-pod.md).

**Question it serves:** a finding of the EKS door sweep, not a register row. The
measurement scripts OOM-killed the Temporal server they read from
([results](2026-11-20-eks-rds-then-the-door.md#5-the-door-sweep)).

## Context

Checked in the code, 2026-11-24:
- Three scripts run `kubectl exec deploy/temporal -- temporal …`:
  - `purchase-cost.sh` (`events`): `workflow list -o jsonl`, for each saga's `historyLength`.
  - `confirm-overlap.sh`: `workflow list -o jsonl` for `ConfirmOrder`.
  - `saga-histories.sh`: one `sh -c` that lists the sagas and runs `workflow show` on
    each into `/tmp/saga-histories` inside the container, then a second exec that
    gzips it out. It is one exec because a kubectl round trip per saga costs seconds.
- The server is `temporalio/auto-setup:1.27.2`. Its Service is `temporal`, on port 7233,
  on both targets (`templates/infra/temporal.yaml`).
- `temporalio/admin-tools:1.27.2-tctl-1.18.2-cli-1.3.0` exists on Docker Hub.
- `deploy/scripts/pg.sh` is the pattern to follow: a sourced function that creates its
  client pod on first use.

## Tasks

### 1. `deploy/scripts/temporal-cli.sh`

A sourced function, `tcli CMD [args…]`. It runs CMD (`temporal`, `sh`, `gzip`) in a
`temporal-cli` pod, creating the pod first if it is absent:
- image: the admin-tools tag above;
- `TEMPORAL_ADDRESS=temporal:7233`;
- `sleep infinity`.

`tcli sh -c '…'` keeps `saga-histories.sh` to a single exec.

**Check first:** `temporal --version` in both the server's container and the CLI pod.
If the CLI versions differ, compare the `-o jsonl` fields the scripts read
(`workflowId`, `historyLength`) on one workflow before switching.

### 2. The three callers use it

Replace each `kubectl exec deploy/temporal --` with `tcli`. `saga-histories.sh`
writes its temporary files in the CLI pod.

**Done when**, on k3s against the same window of one load run, before and after:
- `saga-histories.sh` reports the same saga count;
- `purchase-cost.sh` reports the same history events per saga, over the same count;
- `confirm-overlap.sh` prints the same result;
- during those runs the Temporal pod's restart count does not move, and its
  `container_memory_working_set_bytes` stays flat.

Grep the scripts for `exec deploy/temporal` afterwards: no match.

## Not in scope

The server's memory limit stays at 768Mi; it is no longer the measuring tool's
limit. If the server alone nears it under load, that is a separate finding.
