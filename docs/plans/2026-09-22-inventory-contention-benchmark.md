# Inventory reserve contention benchmark

**Status: COMPLETE 2026-09-22.** `35e864f` is measured. It is real and smaller
than its commit message claims.

**Goal:** Put a number on `35e864f`, which removed `SELECT … FOR UPDATE` from
the reserve path on 2026-09-16 and asserted a throughput change without
measuring one.

**Spec:** none. Derived from a read of `services/inventory-svc/internal/services/reservation.go`
at `35e864f` and `35e864f^`.

## Why the existing harness could not answer this

`deploy/loadtest/purchase.js` drives the whole chain **through the waitroom**,
and `QUEUE_DEFAULT_RELEASE_RATE: "10"` admits ten buyers at a time. Concurrency
at the reserve path is therefore pinned near ten however many VUs are set, and
the lock only binds far above that. Measuring `35e864f` needed a harness that
calls `Reserve` directly.

## Method

`internal/services/reservation_contention_test.go`. 2000 single-ticket reserves
against one ticket class seeded with capacity 1,000,000, so a run never sells
out and every attempt reaches the contended row. Workers fixed at 1, 4 and 16;
three runs each; medians below.

Both versions were run in the same process against the same local Postgres, by
swapping `reservation.go` for its `35e864f^` content and restoring it after.
The rest of `internal/services/` is unchanged since `35e864f`, so the old file
compiles against today's package.

Gated behind `INVENTORY_BENCH=1`. This inverts the convention the rest of the
harness uses — skip locally, fail in CI — deliberately: CI runs under `-race` on
a shared runner, where a throughput number would be noise presented as evidence.

## Results

Reserves per second, median of three runs:

| workers | before (`FOR UPDATE`) | after | change |
|---|---|---|---|
| 1 | 596 | 763 | **1.28×** |
| 4 | 989 | 1302 | **1.32×** |
| 16 | 889 | 1136 | **1.28×** |

Scaling within each version, relative to one worker:

| workers | before | after |
|---|---|---|
| 1 | 1.00× | 1.00× |
| 4 | 1.66× | 1.70× |
| 16 | 1.49× | 1.49× |

## What this says

**The change is real and worth having.** About 30% more throughput at every
concurrency level, for no added risk — the guarded `UPDATE` was already the
arbiter, so the lock it removed was buying nothing.

**The commit title overstates it.** "Hold the ticket-class row for one
statement" — the row lock is taken by the `UPDATE` and held through the
reservation `INSERT` and the `COMMIT`, in both versions. What went away is one
round-trip: the `SELECT … FOR UPDATE`. Three statements under lock became two.
The measured 1.28× is consistent with that, and with `COMMIT` being a fixed cost
that did not change.

**The serialization was not removed.** The commit says throughput was
`1/lock_hold_time` and "adding replicas only added waiters". That sentence
describes the code *after* the change as accurately as before: both versions
peak at four workers and get **slower** at sixteen. `35e864f` reduced the
constant, not the asymptote.

One hot ticket class still costs roughly **1300 reserves/sec, declining past
four concurrent reservers.**

## Consequences

- **`do not autoscale inventory-service` still holds**, and now has a number
  behind it rather than an argument. The guidance in the `deployment-architecture`
  skill and `values-eks.yaml` is unchanged and correct.
- The ceiling is a property of one row, so it is per hot ticket class, not
  per service. An on-sale with several classes is not bounded by this.

## Limits of these numbers

- Local Docker Postgres on a laptop, not RDS. **The ratio is the finding; the
  absolute numbers are not production numbers.**
- The DB path only: no gRPC, no serialization, no network.
- `MaxOpenConns: 25`, so at 16 workers the pool is not the binding constraint.
- `PrepareStmt: true` is on, as in production.

## Found, not fixed

- **Confirm also holds the hot row, and is unmeasured.** `confirmReservationTx`
  mutates `ticket_class` with a guarded `UPDATE` (`WHERE id = ? AND reserved >= ?`)
  and holds that row to `COMMIT`, exactly as `Reserve` does. Every successful
  purchase pays it, so the real per-class ceiling is lower than the reserve
  number alone suggests.

  Its `SELECT … FOR UPDATE` (`reservation.go:186`, `:296`) is **not** part of
  that: it locks `reservation` rows scoped to one `order_code`, and two buyers
  never share an order code, so those locks do not contend across buyers.
- **`pkg/gorm` hardcodes `LogLevel: logger.Info`**, writing a line per statement
  synchronously, in production as well as tests. The benchmark had to silence it
  to stop measuring log I/O. Nothing sets it per environment.
