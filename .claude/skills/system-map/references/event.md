# Event — reading guide

For events, organisers, event configuration, the lifecycle state machine, and the
rules the waitroom and the saga read from an event.

## Read in this order

1. `services/event-svc/CLAUDE.md` — **current**: the lifecycle, the non-contiguous
   proto enum and why it must go through the mapper, layout, and the absence of CQRS.
2. `docs/plans/2026-09-21-event-svc-domain-tests.md` — what the domain tests pin, and
   the defects they found. A record of that day.
3. Root `CLAUDE.md`, *Canonical TS layout* — which calls `event-svc` the reference
   and also forbids a split `event-svc` has. That contradiction is open; don't
   resolve it by copying either shape into another service.

## By question

| Question | Owner | Verify in |
|---|---|---|
| Which status transitions are legal, and who may make them | service `CLAUDE.md`, *Event lifecycle* | `services/event-svc/src/modules/events/events.service.ts` |
| Mapping status between proto and Prisma | service `CLAUDE.md` | `services/event-svc/src/modules/events/controllers/grpc/mappers/event-status.mapper.ts` |
| What an event and its config hold | `services/event-svc/prisma/schema/schema.prisma` | same |
| What the waitroom asks, and how often | [waitroom.md](waitroom.md), the event cache | `services/waitroom-svc/internal/service/event_gate.go` |
| What the saga asks | [order.md](order.md) | `services/order-svc/internal/activities/event_activity.go` |
| Demo and dev data | `services/event-svc/prisma/seed.ts` | same |

## Code entry points

| To see | Open |
|---|---|
| The gRPC surface and its request DTOs | `services/event-svc/src/modules/events/controllers/grpc/events.controller.ts` |
| Data access | `services/event-svc/src/modules/events/repository/events.repository.ts` |
| Domain DTOs and entities | `services/event-svc/src/modules/events/dtos/`, `services/event-svc/src/modules/events/entities/` |

## Edges

- **Serves** `proto/event.proto` — to the gateway, order-svc and waitroom-svc.
- **Calls** nothing; no Kafka.
- **Store** — Postgres via Prisma; migrations in `services/event-svc/prisma/migrations/`.
- **Deployed by** `deploy/helm/ticketbottle/templates/apps/event.yaml`.
