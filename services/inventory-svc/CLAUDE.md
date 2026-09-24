# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **Inventory** service for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role

High-throughput, gRPC-only ticket inventory (port **50057**, PostgreSQL via GORM). It is the correctness-critical heart of overselling prevention: every write to a `ticket_class` counter (`reserved`/`sold`) either runs inside a **`SELECT ... FOR UPDATE`** pessimistic-lock transaction (`tx.Clauses(clause.Locking{Strength: "UPDATE"})`), or is a **guarded conditional `UPDATE`** whose `WHERE` predicate makes the write incapable of violating the capacity constraint (`internal/services/reservation.go`).

## Three-step reservation flow

```
Reserve  → guarded UPDATE per class, hold quantity for ~9 min (PaymentTimeout + ReservationHoldGrace, set by order-svc), create Reservation
Confirm  → convert a held reservation into a sale (decrement reserved, increment sold)
Release  → free a held reservation
```

Keyed by **order code**. A background `ReservationExpiryWorker` (`internal/workers/`) auto-releases holds that expire, so the Order saga's compensation and the worker can both free inventory safely. `Reserve` runs as a **single transaction** and takes **no `SELECT ... FOR UPDATE`**. It issues one guarded `UPDATE` per class in **ascending id order** (deadlock-free), then batch-inserts the reservation rows — all-or-nothing.

**Eligibility and availability live in that `UPDATE`'s predicate** — `status = ACTIVE`, `now` within `[sale_start_at, sale_end_at]` (either bound may be null), and `reserved + sold + qty <= total`. Under READ COMMITTED Postgres re-checks the predicate against the newest committed row after taking its own lock, so `RowsAffected == 0` is a correct verdict without a prior read. `explainReserveMiss` then reads the row *unlocked* to name which of the three reasons it was: `ErrNotFound`, `ErrSaleClosed` → gRPC `FailedPrecondition`, or `ErrInsufficientStock`. `CheckAvailability` applies the same eligibility rule.

**The row is still held from that `UPDATE` to `COMMIT`**, so one hot ticket class is processed serially. Measured: ~1300 reserves/sec, peaking at four concurrent reservers and declining above that. Numbers and method in `docs/plans/2026-09-22-inventory-contention-benchmark.md`. Spreading the same concurrency over eight rows gave about 2×, not 8×, and disabling `synchronous_commit` about a tenth more (`b22d268`, `TestReserveThroughput_SpreadAcrossClasses`): the row lock is a wall, not the only one. Do not autoscale this service expecting a hot class to go faster.

## Commands

```bash
make run          # go run cmd/api/main.go
make protoc       # regenerate protobuf
make update-proto # regenerate gRPC stubs from the root proto/
make test         # run tests against a live Postgres (see below; go test ./internal/services/... for one package)
go build ./...
```

Use `make test`, not bare `go test ./...`: without a reachable Postgres on 5435, `go test ./...` silently `t.Skipf`s every DB-backed test and reports PASS having asserted nothing. `make test` runs `test-db` first (creates `ticketbottle_inventory_test` against the `ticketbottle-inventory` container) so the suite actually executes. `setup_test.go` hard-fails instead of skipping when `CI` is set, and `.github/workflows/go-tests.yml` exercises that guard on every push, with Postgres as a service container.

The contention measurement in `reservation_contention_test.go` inverts that rule: it skips unless `INVENTORY_BENCH=1`, because CI runs under `-race` where a throughput number would be noise presented as evidence. Run it with `INVENTORY_BENCH=1 go test ./internal/services/ -run TestReserve(Throughput|UnderContention) -v`.

On boot `main.go` runs GORM `AutoMigrate` for `TicketClass` and `Reservation`, then applies `models.PostMigrateStatements()` (`internal/models/ddl.go`) — the single source of DDL that AutoMigrate cannot express. That is the partial index `idx_reservation_active_expiry` plus three `CHECK` constraints:

- `chk_ticket_class_capacity` — `reserved >= 0 AND sold >= 0 AND reserved + sold <= total`
- `chk_ticket_class_total_nonneg` — `total >= 0`
- `chk_reservation_qty_positive` — `qty > 0`

They are added `NOT VALID`: enforced on every new write, but historical rows are not scanned, so pre-existing drift cannot fail a boot. Validate deliberately, out of band, once drift is known clean: `ALTER TABLE ticket_class VALIDATE CONSTRAINT chk_ticket_class_capacity;`

All of this is still interim — versioned migrations are the target. Default Postgres is on **5435** (see `config/config.go`).

## Layout

- `cmd/api/main.go` — wiring: config → zap logger → GORM → repo → services → workers → gRPC server.
- `internal/delivery/grpc/` — gRPC handlers; `internal/services/` — `reservation.go` (locking/flow) + `ticketclass.go`; `internal/workers/` — expiry worker + manager; `internal/models/`.
- `pkg/` — shared `gorm`, `grpc`, `logger`, `errors`, `response`, `util`.

## Conventions

- Logging uses the zap wrapper with ctx-first `f`-suffixed methods: `s.l.Errorf(ctx, "service.reservation.Reserve: %v", err)`. Prefix messages with `package.type.Method` as the existing code does.
- The real invariant is not "always `FOR UPDATE`": `Confirm`, `Release`, and `BatchExpireReservations` lock `reservation` rows `FOR UPDATE` but mutate `ticket_class` via a **guarded conditional `UPDATE`** (e.g. `WHERE id = ? AND reserved >= ?`), not a locked read-then-write of `ticket_class` itself. A `ticket_class` counter write must be inside a `FOR UPDATE` transaction on that row, **or** be a guarded conditional `UPDATE` whose predicate makes the write incapable of violating the capacity constraint — never a bare read-modify-write of `reserved`/`sold`/`total` outside either.
- When locking multiple `ticket_class` rows, always lock in ascending id order.
- **Errors crossing the service boundary are domain errors**, never GORM sentinels: `ErrInsufficientStock`, `ErrNotFound`, `ErrStateConflict`, `ErrSaleClosed`, `ErrInventoryDrift` (`internal/services/errors.go`). `internal/delivery/grpc/errors.go` maps them to gRPC codes. Returning `gorm.ErrInvalidData` to mean "sold out" is how this service used to mistranslate unrelated driver failures into `ResourceExhausted`.
- **`ErrInventoryDrift` means corruption, not a user error** — `reserved` is lower than the holds claiming it, which can only happen if something wrote a quantity outside a locked transaction. The expiry worker skips drifted ticket classes and leaves their reservations `ACTIVE` so the evidence survives and the error log repeats every tick.
- **Never write `reserved` or `sold` from the ticket-class CRUD path.** `updateColumns` deliberately cannot express them; `Update` locks `FOR UPDATE` and refuses to shrink `total` below `reserved + sold`.
- **Tests need a live Postgres.** `make test` starts from `docker-compose.dev.yml` (container `ticketbottle-inventory`, port 5435) and creates `ticketbottle_inventory_test`. The harness skips locally when the DB is unreachable but **fails** when `CI` is set — a suite that skips itself reports PASS having asserted nothing.

### Idempotency

`Reserve`/`Confirm`/`Release` key off `order_code`, and idempotency is scoped **by reservation status** — an order code alone is not enough to decide.

- **`Reserve`** — an order with any `ACTIVE` or `CONFIRMED` row is a retry of a call that already succeeded: no-op, success. An order whose rows are **all terminal** (`EXPIRED`/`CANCELLED`) returns `ErrStateConflict`; returning success there would hand the caller an order holding zero inventory.
- **`Confirm`** — all-`CONFIRMED` is a no-op. An `ACTIVE` row **past its `expires_at` is still confirmed**: it continues to hold its `reserved` quantity until the worker sweeps it, so the reserved→sold move is safe and refusing it stranded paid orders. An `EXPIRED` row (already swept) is **re-acquired** from free stock; only if the stock is genuinely gone does it return `ErrStateConflict`, which the caller must treat as refund-required.
- **`Release`** — an unknown order code or all-terminal rows are a no-op; a `CONFIRMED` row returns `ErrStateConflict`.

Order-svc sets the hold to `PaymentTimeout + ReservationHoldGrace` (`internal/workflows/shared.go`) so the hold strictly outlives the payment window — the expiry worker must never win that race.

## Design docs

Rationale that does not fit a comment lives here, and comments point at it
(see the root `CLAUDE.md`, "Comment conventions"):

- `docs/POST_MIGRATE_DDL.md` — the post-`AutoMigrate` statements: why every constraint is `NOT VALID`, and the guarded `fk_ticket_class_reservations` repair.
