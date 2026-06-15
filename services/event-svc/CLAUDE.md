# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **Event** service for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role

gRPC microservice (port **50053**) owning events, organizers, and their configuration. Data lives in PostgreSQL via Prisma. It enforces the event **lifecycle state machine** and is queried by Order/Waitroom (e.g. to validate an event before reserving).

## Event lifecycle

Status transitions are driven in `src/modules/events/events.service.ts` via repository updates:

```
DRAFT → CONFIGURED → APPROVED → PUBLISHED
```

(Proto enum values are non-contiguous — `DRAFT=1, PUBLISHED=2, CONFIGURED=4, APPROVED=5` — so always map through `src/modules/events/controllers/grpc/mappers/event-status.mapper.ts`, never compare proto and Prisma enums directly.)

## Commands

```bash
npm install
npm run start:dev          # watch mode
npm run start:prod
npm run build
npm run lint               # eslint --fix
npm run test               # jest  (npm run test -- <path> for one file)
npm run prisma-migrate:dev
npm run prisma:seed        # ts-node prisma/seed.ts
npm run proto:all          # regenerate gRPC stubs into src/protogen
```

## Layout

- `src/modules/events/` — `events.service.ts` (business logic + lifecycle), `repository/` (Prisma data access), `controllers/grpc/` (+ `mappers/`), `dtos/`, `entities/`.
- `src/infra/database/` — Prisma infrastructure; `src/common/*` — gRPC exception filter + validation; `src/shared/*` — config/logger/swagger.
- `prisma/` — schema, migrations, `seed.ts`. `src/protogen/` is generated from `../../proto/event.proto` — don't hand-edit.

## Notes

- Despite the root README mentioning CQRS, this service does **not** use `@nestjs/cqrs` — it is a plain service + repository. Don't add CQRS scaffolding expecting it to already exist.
- Role-based access (organizer vs admin) gates lifecycle transitions; keep authorization checks in the service layer alongside the status update.
