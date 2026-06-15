# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **Inventory** service for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role

High-throughput, gRPC-only ticket inventory (port **50057**, PostgreSQL via GORM). It is the correctness-critical heart of overselling prevention: every quantity change goes through a **`SELECT ... FOR UPDATE`** pessimistic lock inside a transaction (`tx.Clauses(clause.Locking{Strength: "UPDATE"})` in `internal/services/reservation.go`).

## Three-step reservation flow

```
Reserve  → lock ticket rows, hold quantity for ~15 min, create Reservation
Confirm  → convert a held reservation into a sale (decrement reserved, increment sold)
Release  → free a held reservation
```

Keyed by **order code**. A background `ReservationExpiryWorker` (`internal/workers/`) auto-releases holds that expire, so the Order saga's compensation and the worker can both free inventory safely. `Reserve` fans out per item over goroutines, so each item locks independently.

## Commands

```bash
make run          # go run cmd/api/main.go
make protoc       # regenerate protobuf
make update-proto # pull latest from protos-submodule then regenerate
go test ./...     # run tests (go test ./internal/services/... for one package)
go build ./...
```

On boot `main.go` runs GORM `AutoMigrate` for `TicketClass` and `Reservation` — there are no separate migration files. Default Postgres is on **5435** (see `config/config.go`).

## Layout

- `cmd/api/main.go` — wiring: config → zap logger → GORM → repo → services → workers → gRPC server.
- `internal/delivery/grpc/` — gRPC handlers; `internal/services/` — `reservation.go` (locking/flow) + `ticketclass.go`; `internal/workers/` — expiry worker + manager; `internal/models/`.
- `pkg/` — shared `gorm`, `grpc`, `logger`, `errors`, `response`, `util`.

## Conventions

- Logging uses the zap wrapper with ctx-first `f`-suffixed methods: `s.l.Errorf(ctx, "service.reservation.Reserve: %v", err)`. Prefix messages with `package.type.Method` as the existing code does.
- Never read-modify-write a quantity outside the `FOR UPDATE` transaction — that is the invariant preventing oversell.
