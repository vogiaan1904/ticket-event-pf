# Inventory Service Optimization — Design

**Date:** 2026-07-13
**Service:** `services/inventory-svc` (Go, gRPC, PostgreSQL via GORM, port 50057)
**Status:** Approved design → ready for implementation plan

## Context

The inventory service is the correctness-critical heart of oversell prevention.
Every quantity change is meant to go through a `SELECT … FOR UPDATE` pessimistic
lock inside a transaction. A review of the current implementation found that the
locking primitives are used correctly, but the **orchestration around them** — the
`Reserve` flow, saga retry handling, the expiry worker, schema management, and a
large body of dead code — has real correctness bugs and rough edges.

### On GORM vs. raw SQL (the original question)

**GORM stays.** The service's correctness lives in its concurrency strategy, not
its query builder, and that strategy is expressed explicitly and correctly through
GORM:

- `tx.Clauses(clause.Locking{Strength: "UPDATE"})` → real `SELECT … FOR UPDATE`.
- `gorm.Expr("reserved + ?", qty)` with a `reserved >= ?` WHERE guard → atomic
  conditional updates, no read-modify-write races.
- `Locking{Strength:"UPDATE", Options:"SKIP LOCKED"}` in the expiry worker →
  standard queue-draining pattern.

GORM is acting as a thin typed wrapper over exactly the SQL we'd hand-write.
Switching to raw SQL would add boilerplate and more places to typo a lock clause
without improving safety. The problems below are orthogonal to the ORM choice.

## Goals

1. Make `Reserve` atomic and free of the data race, capacity leak, and deadlock
   risk in the current goroutine fan-out.
2. Make `Reserve` / `Confirm` / `Release` idempotent under Temporal activity
   retries.
3. Make the expiry worker keep up with backlog and use an appropriate index.
4. Replace boot-time `AutoMigrate` with versioned migrations, mirroring the
   per-service migrate-Job pattern already used by the Prisma services.
5. Remove dead code and tighten the service to convention.
6. Add the tests that don't exist today, centered on the concurrency-critical core.

## Non-goals

- **Kafka events.** `reservation.cancelled` / `reservation.expired` have TODOs but
  no consumer today (Order frees inventory synchronously over gRPC). Adding a
  producer now is speculative; leave the TODOs. Out of scope.
- Changing the gRPC contract (`proto/`). The RPC surface stays as-is.
- Reworking `TicketClass` CRUD semantics beyond dead-code removal.

## Current-state facts (verified during review)

- `Reserve` (`internal/services/reservation.go:38`) spawns one goroutine **and one
  transaction per item**, writing a shared `wgErr` with no mutex.
- On partial failure it calls `DeleteByOrderCode`, which deletes reservation rows
  but **never decrements `ticket_class.reserved`** — a permanent phantom reserved
  count that the expiry worker can't recover (the row is gone).
- `Confirm` on an already-`CONFIRMED` order returns `gorm.ErrInvalidData`, which a
  retrying Temporal activity sees as failure.
- The expiry worker processes one batch (≤500) per 1-minute tick.
- The expiry query filters `status = ? AND expires_at < ?` but the only composite
  index `idx_ticket_status_expires` leads with `ticket_class_id`, so the scan can't
  use it.
- **No tests exist** (`find … -name '*_test.go'` → none).
- Dead code (verified: only `DeleteByOrderCode` had a caller, inside the Reserve
  compensation being removed): `ReservationService.{UpdateStatus,
  UpdateStatusByOrderCode, Delete, DeleteByOrderCode}` plus unexported-in-intent
  impl methods `{GetByOrderCode, GetActiveByTicketClassID, GetExpired,
  GetTotalReservedQuantity}`; `TicketClassService.{GetByEventID, IncrementReserved,
  DecrementReserved, IncrementSold}`; several `pkg/gorm/repository.go` generic
  helpers (incl. `Exists`, which panics on empty `conditions`); `getEnvAsSlice` /
  `getEnvAsBool` in `config.go`.
- `GetExpired` uses `<=` while `BatchExpireReservations` uses `<` for the same
  concept.
- Migration pattern to mirror: `deploy/helm/ticketbottle/templates/apps/migrations.yaml`
  runs a `{svc}-migrate` Job (`helm.sh/hook: post-install,post-upgrade`, a
  `wait-postgres` init container) for `user`/`event`/`payment` via
  `npx prisma migrate deploy`; images built by `deploy/scripts/build-migrate-images.sh`.

## Design

### S1 — `Reserve` becomes one atomic transaction

Rewrite `Reserve` as a single `s.repo.GetDB().WithContext(ctx).Transaction(...)`:

1. **Idempotency guard (see S2):** if reservation rows already exist for
   `order_code`, return `nil` without inserting.
2. **Deterministic locking:** collect the distinct `ticket_class_id`s from the
   request, and lock them in ascending id order:
   `tx.Clauses(clause.Locking{Strength:"UPDATE"}).Where("id IN ?", sortedIDs).Order("id").Find(&tcs)`.
   Locking every transaction's rows in the same global order eliminates the AB/BA
   deadlock the goroutine version could hit. (In PostgreSQL the `LockRows` node sits
   above `Sort`, so `ORDER BY id … FOR UPDATE` locks in id order.)
3. **Validate against the locked rows:** for each item, compute
   `available = Total - Reserved - Sold` and fail the whole transaction with
   `gorm.ErrInvalidData` (→ insufficient stock) if any item can't be satisfied.
4. **Increment counters:** per item, `UPDATE ticket_class SET reserved = reserved + ?
   WHERE id = ? AND reserved + sold + ? <= total`. The WHERE guard is defense in
   depth; correctness already holds because the row is locked. `RowsAffected == 0`
   → fail.
5. **Batch insert** all reservation rows in one `tx.Create(&rs)`.

Any error rolls back everything. **Delete** `Create`, `DeleteByOrderCode`, the
`sync.WaitGroup`, and `wgErr`. Result: atomic multi-item reservation, no data race,
no capacity leak, no compensation round-trip, one DB connection per order.

Sketch:

```go
func (s implReservationService) Reserve(ctx context.Context, in ReserveInput) error {
    return s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // 1. Idempotency: already reserved for this order?
        var existing int64
        if err := tx.Model(&models.Reservation{}).
            Where("order_code = ?", in.OrderCode).Count(&existing).Error; err != nil {
            return err
        }
        if existing > 0 {
            return nil // no-op retry
        }

        // 2. Lock all target ticket classes in a deterministic order.
        ids := sortedDistinctTicketClassIDs(in.Items)
        var tcs []models.TicketClass
        if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
            Where("id IN ?", ids).Order("id").Find(&tcs).Error; err != nil {
            return err
        }
        if len(tcs) != len(ids) {
            return gorm.ErrRecordNotFound // an unknown ticket class was requested
        }
        byID := indexByID(tcs)

        // 3 + 4. Validate and increment, guarded.
        for _, it := range in.Items {
            tc := byID[it.TicketClassID]
            if tc.Total-tc.Reserved-tc.Sold < it.Qty {
                return gorm.ErrInvalidData // insufficient stock
            }
        }
        for _, it := range in.Items {
            res := tx.Model(&models.TicketClass{}).
                Where("id = ? AND reserved + sold + ? <= total", it.TicketClassID, it.Qty).
                Update("reserved", gorm.Expr("reserved + ?", it.Qty))
            if res.Error != nil {
                return res.Error
            }
            if res.RowsAffected == 0 {
                return gorm.ErrInvalidData
            }
        }

        // 5. Batch insert reservations.
        rs := s.buildModels(in.OrderCode, in.ExpiresAt, in.Items)
        return tx.Create(&rs).Error
    })
}
```

### S2 — Idempotent saga semantics

Temporal retries activities, including after a committed-but-unacked call. Each
operation keys off `order_code` and maps "already in the target state / nothing to
do" to **success**; only genuine state conflicts return an error.

| RPC | State found for `order_code` | Result |
|-----|------------------------------|--------|
| `Reserve` | rows already exist | `nil` (no-op) — S1 guard |
| `Reserve` | no rows | create atomically |
| `Confirm` | all `CONFIRMED` | `nil` |
| `Confirm` | all `ACTIVE` (not expired) | move reserved→sold, set `CONFIRMED` |
| `Confirm` | any `EXPIRED`/`CANCELLED`, or expired-by-time | `ErrConflict` |
| `Confirm` | no rows | `ErrRecordNotFound` |
| `Release` | all `CANCELLED`/`EXPIRED`, or no rows | `nil` (no-op) |
| `Release` | `ACTIVE` present | decrement reserved, set `CANCELLED` |
| `Release` | any `CONFIRMED` | `ErrConflict` |

In `confirmReservationTx` / `cancelReservationTx`, after locking the rows, branch on
the aggregate status before mutating. Add `ErrConflict = NewGRPCError(codes.FailedPrecondition,
"reservation state conflict")` in `pkg/errors/errors.go`, and map `gorm`/domain
signals to it in `delivery/grpc/errors.go` so a conflict is distinguishable from
`ErrInsufficientStock` (`codes.ResourceExhausted`).

### S3 — Expiry worker draining + hot-path index

- **Drain loop:** in `ReservationExpiryWorker.runJob`, call
  `BatchExpireReservations` in a loop until it returns `< batchSize` (fully drained)
  or a `maxIterations` safety cap is hit, so a backlog larger than `batchSize` per
  tick doesn't accumulate. Each call remains its own transaction with `SKIP LOCKED`,
  so multiple ticks/instances stay safe.
- **Index:** add a partial index
  `CREATE INDEX idx_reservation_active_expiry ON reservation (status, expires_at) WHERE status = 'ACTIVE';`
  to serve the worker's `WHERE status=? AND expires_at<=? ORDER BY expires_at` scan
  and its `FOR UPDATE SKIP LOCKED` lock. The existing `idx_ticket_status_expires`
  stays only if a surviving query needs it; otherwise remove it as part of S5.
- **Boundary:** standardize the expiry comparison on `expires_at <= now` everywhere.

### S4 — Versioned migrations, drop `AutoMigrate` (separable phase)

Mirror the existing per-service migrate-Job pattern with a Go tool (**goose**).

- `services/inventory-svc/migrations/*.sql` — goose SQL migrations embedded via
  `embed.FS`.
  - `00001_init.sql`: reproduce today's schema exactly as `AutoMigrate` produces it:
    - `ticket_class` (id bigserial PK, event_id NOT NULL, name NOT NULL, price_cents
      bigint NOT NULL, currency NOT NULL, total int NOT NULL, reserved int NOT NULL
      DEFAULT 0, sold int NOT NULL DEFAULT 0, sale_start_at timestamptz, sale_end_at
      timestamptz, status NOT NULL DEFAULT 'ACTIVE', created_at, updated_at);
      unique index `idx_event_name (event_id, name)`, index `idx_event_id (event_id)`.
    - `reservation` (id bigserial PK, order_code NOT NULL, ticket_class_id bigint NOT
      NULL, qty int NOT NULL, expires_at timestamptz NOT NULL, status NOT NULL,
      created_at, updated_at); unique index `idx_order_ticket (order_code,
      ticket_class_id)`, FK `ticket_class_id → ticket_class(id) ON DELETE RESTRICT`.
    - Keep or drop `idx_ticket_status_expires` consistent with S3/S5.
  - `00002_active_expiry_index.sql`: the S3 partial index.
- `cmd/migrate/main.go` — runs `goose up` (and supports `down`/`status`) against
  `POSTGRES_URL`, migrations from the embedded FS.
- `Dockerfile.migrate` — builds an `inventory-migrate` image around the `migrate`
  binary.
- `cmd/api/main.go` — remove the `db.AutoMigrate(...)` block; `main` only connects.
- Deploy: add an `inventory-migrate` Job. The existing `migrations.yaml` hardcodes
  `npx prisma migrate deploy`, so add a parallel Go block (or a second template) with
  `command: ["/migrate","up"]`, the same `post-install,post-upgrade` hook, weight,
  and `wait-postgres` init container. Add inventory to `build-migrate-images.sh`.
- Local dev: add a `make migrate` target (`go run ./cmd/migrate up`) and run it in the
  `up-aws` flow before the service starts.

This phase is the most independent and has the largest blast radius into `deploy/`.
It can be split into its own follow-up spec/plan without blocking S1–S3, S5–S6; if
split, keep `AutoMigrate` until the migration Job lands so the service still boots.

### S5 — Dead-code and convention sweep

Remove, each re-verified with a grep for callers immediately before deletion:

- `ReservationService` interface + impl: `UpdateStatus`, `UpdateStatusByOrderCode`,
  `Delete`, `DeleteByOrderCode`, and impl-only `GetByOrderCode`,
  `GetActiveByTicketClassID`, `GetExpired`, `GetTotalReservedQuantity`, `Create`
  (folded into S1). Keep `sumQuantities` only if still referenced after the rewrite.
- `TicketClassService` interface + impl: `GetByEventID`, `IncrementReserved`,
  `DecrementReserved`, `IncrementSold` (all unused; the atomic paths live in S1/
  `confirm`/`cancel`).
- `pkg/gorm/repository.go`: drop unused generic helpers; at minimum fix or remove
  `Exists` (panics on empty `conditions`). Keep only helpers the services actually
  call.
- `config.go`: remove `getEnvAsSlice`, `getEnvAsBool`.
- Delete the redundant second availability check that existed in old `Create`.

### S6 — Tests

No tests exist today. Add, against a **real Postgres** (testcontainers-go, or the
compose DB in CI) because `FOR UPDATE` / `SKIP LOCKED` semantics cannot be
exercised on SQLite:

- **Oversell prevention (the money test):** N concurrent `Reserve` calls on a
  ticket class with capacity C < N·qty; assert `reserved + sold` never exceeds
  `total` and exactly the right number succeed. Run under `go test -race`.
- **Deadlock freedom:** concurrent multi-class reservations on overlapping classes in
  opposing request orders complete without deadlock errors.
- **Happy paths:** Reserve → Confirm (reserved→sold), Reserve → Release (reserved
  freed).
- **Idempotency:** repeated Reserve / Confirm / Release for the same `order_code`
  per the S2 table.
- **Expiry:** active-but-expired holds are released and marked `EXPIRED`; drain loop
  clears a backlog larger than `batchSize` in one `runJob`.

## Phasing

1. **Phase A — service-internal correctness (S1, S2, S5, S6).** Self-contained; no
   deploy changes. Highest value.
2. **Phase B — expiry worker + index (S3).** Small; the index ships as a migration
   if S4 lands first, otherwise as an `AutoMigrate` model-tag change.
3. **Phase C — migrations + deploy (S4).** Separable; can become its own spec/plan.

## Risks & mitigations

- **Behavioral change in error mapping (S2).** New `FailedPrecondition` conflicts
  where callers previously saw `ResourceExhausted`/`Internal`. Confirm the Order
  saga treats conflict as non-retryable; coordinate if it currently keys on the old
  codes.
- **Initial migration drift (S4).** `00001_init.sql` must match the live
  `AutoMigrate` schema exactly. Verify by diffing a fresh `goose up` database against
  a fresh `AutoMigrate` database (`pg_dump --schema-only`) before switching prod.
- **Lock-order assumption (S1).** Deadlock freedom relies on every writer locking
  `ticket_class` rows in ascending id order. The `Confirm`/`Release` paths lock
  `reservation` rows by `order_code`, a disjoint set, so they don't conflict with the
  `ticket_class` ordering; document the invariant in `CLAUDE.md`.
- **Test infra (S6).** Requires Postgres in CI. If testcontainers is undesirable,
  gate the concurrency tests behind a build tag that points at the compose DB.

## Documentation to update on completion

- `services/inventory-svc/CLAUDE.md`: the atomic single-transaction `Reserve`
  (replaces the "fans out per item over goroutines" description), the idempotency
  contract, the ascending-id lock-order invariant, and migrations replacing
  `AutoMigrate`.
- Root `CLAUDE.md`: note inventory now uses versioned migrations (if S4 lands).
