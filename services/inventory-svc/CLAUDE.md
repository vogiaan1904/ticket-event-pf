# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **Inventory** service for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role

High-throughput, gRPC-only ticket inventory (port **50057**, PostgreSQL via GORM). It is the correctness-critical heart of overselling prevention: every write to a `ticket_class` counter (`reserved`/`sold`) either runs inside a **`SELECT ... FOR UPDATE`** pessimistic-lock transaction (`tx.Clauses(clause.Locking{Strength: "UPDATE"})`), or is a **guarded conditional `UPDATE`** whose `WHERE` predicate makes the write incapable of violating the capacity constraint (`internal/services/reservation.go`).

## Three-step reservation flow

```
Reserve  → lock ticket rows, hold quantity for ~9 min (PaymentTimeout + ReservationHoldGrace, set by order-svc), create Reservation
Confirm  → convert a held reservation into a sale (decrement reserved, increment sold)
Release  → free a held reservation
```

Keyed by **order code**. A background `ReservationExpiryWorker` (`internal/workers/`) auto-releases holds that expire, so the Order saga's compensation and the worker can both free inventory safely. `Reserve` runs as a **single transaction**: it locks all target `ticket_class` rows in **ascending id order** (deadlock-free), validates sale eligibility and availability, increments `reserved`, and batch-inserts the reservation rows — all-or-nothing.

**Sale eligibility** is enforced inside that locked transaction: the ticket class must be `ACTIVE` and `now` must fall within `[sale_start_at, sale_end_at]` (either bound may be null). Violations return `ErrSaleClosed` → gRPC `FailedPrecondition`. `CheckAvailability` applies the same rule.

## Commands

```bash
make run          # go run cmd/api/main.go
make protoc       # regenerate protobuf
make update-proto # regenerate gRPC stubs from the root proto/
make test         # run tests against a live Postgres (see below; go test ./internal/services/... for one package)
go build ./...
```

Use `make test`, not bare `go test ./...`: without a reachable Postgres on 5435, `go test ./...` silently `t.Skipf`s every DB-backed test and reports PASS having asserted nothing. `make test` runs `test-db` first (creates `ticketbottle_inventory_test` against the `ticketbottle-inventory` container) so the suite actually executes. `setup_test.go` also hard-fails instead of skipping when the `CI` env var is set, but that guard is currently unreachable in practice — this repo has no root `.github/workflows`, so nothing ever sets `CI` here. Treat it as aspirational until a CI workflow exists.

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
