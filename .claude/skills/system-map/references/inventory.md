# Inventory — reading guide

*Trust labels checked against the code on 2026-09-23.*

For ticket classes, `Reserve` / `Confirm` / `Release`, the expiry worker, the schema
constraints, and the throughput of one hot ticket class.

## Read in this order

1. `services/inventory-svc/CLAUDE.md` — **current, and the owner of the reserve
   path**: how each write stays within capacity, idempotency by reservation
   status, the domain errors, the post-migrate DDL, and why tests need a live
   Postgres.
2. `docs/plans/2026-09-22-inventory-contention-benchmark.md` — how the ceiling on
   one hot ticket class was measured, and the numbers. A record of that day.
3. `services/inventory-svc/docs/POST_MIGRATE_DDL.md` — **current**: why every
   constraint is added `NOT VALID`, and the guarded foreign-key repair.
4. `services/order-svc/docs/RESERVATION_HOLD.md` — the hold's length is set by
   order-svc, not here.

The schema is owned by `services/inventory-svc/internal/models/` and
`services/inventory-svc/internal/models/ddl.go`, not by any document.

## By question

| Question | Owner | Verify in |
|---|---|---|
| What keeps a sale within capacity | service `CLAUDE.md`, *Three-step reservation flow* and *Conventions* | `services/inventory-svc/internal/services/reservation.go` |
| What bounds throughput on one class | service `CLAUDE.md`; the benchmark plan | `services/inventory-svc/internal/services/reservation_contention_test.go` |
| Whether `Confirm` contends enough to matter | root `CLAUDE.md`, register — **open** | `services/inventory-svc/internal/services/reservation.go` |
| A retried `Reserve`, `Confirm` or `Release` | service `CLAUDE.md`, *Idempotency* | `services/inventory-svc/internal/services/reservation.go` |
| When expired holds are released, and what stops it | service `CLAUDE.md`, *Conventions* | `services/inventory-svc/internal/workers/reservation_exp_worker.go` |
| Which domain error becomes which gRPC code | service `CLAUDE.md`, *Conventions* | `services/inventory-svc/internal/services/errors.go`, `services/inventory-svc/internal/delivery/grpc/errors.go` |
| Schema, indexes, CHECK constraints | `services/inventory-svc/docs/POST_MIGRATE_DDL.md` | `services/inventory-svc/internal/models/ddl.go` |
| Whether to autoscale it | `.claude/skills/deployment-architecture/SKILL.md`, and its EKS reference, *scaling limits* | `deploy/helm/ticketbottle/templates/apps/inventory.yaml` |

## Code entry points

| To see | Open |
|---|---|
| Wiring, migrations on boot, workers | `services/inventory-svc/cmd/api/main.go` |
| The gRPC surface | `services/inventory-svc/internal/delivery/grpc/service.go` |
| Ticket-class CRUD | `services/inventory-svc/internal/services/ticketclass.go` |
| Models | `services/inventory-svc/internal/models/` |

## Edges

- **Serves** `proto/inventory.proto` — to order-svc
  (`services/order-svc/internal/activities/inventory_activity.go`). The gateway's
  module is an empty stub (`services/api-gateway/src/modules/inventory/inventory.service.ts`).
- **Calls** nothing; no Kafka.
- **Store** — Postgres, database `ticketbottle_inventory`, via GORM.
- **Seeded by** `deploy/scripts/seed-ticketclass.sh` on a cluster.
- **Deployed by** `deploy/helm/ticketbottle/templates/apps/inventory.yaml`.
