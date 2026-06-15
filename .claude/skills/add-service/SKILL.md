---
name: add-service
description: Use when adding a brand-new microservice to TicketBottle — choosing the stack, following the canonical per-stack layout, wiring its proto contract, env file, and docker-compose entry. Keeps the new service consistent with the existing seven.
---

# Adding a new service

New services live under `services/<name>-svc/` (the gateway is the only one without the `-svc` suffix). Pick the stack by role: **Go** for hot-path/throughput-critical or stateful-coordination services; **NestJS (TS)** for CRUD/domain services with Prisma.

## 1. Contract
Add `proto/<name>.proto` (one service per file, mirror the existing style). Regenerate consumers with the `proto-change` skill.

## 2. Scaffold the canonical layout

**Go** (mirror `inventory-svc`/`waitroom-svc`, not `order-svc` which has extra Temporal layers):
```
cmd/<binary>/main.go        # wiring: config → zap logger → datastore → repo → service → gRPC server
internal/delivery/grpc/     # thin handlers
internal/service(s)/        # business logic
internal/repository/        # data access
internal/models/
config/                     # config.go
pkg/                        # logger, errors, grpc, response, util (reuse the existing wrapper conventions)
```
Logging: zap wrapper, ctx-first `f`-methods. Use `fmt.Errorf` freely (the error-var rule is order-svc-only).

**NestJS** (mirror the **Canonical TS layout** in root `CLAUDE.md` — do NOT copy event-svc's parallel `controllers/grpc/dtos` + `dtos` split):
```
src/main.ts                 # NestFactory.createMicroservice, Transport.GRPC
src/modules/<feature>/      # controller + service + module + dto/ + repository.ts
src/common/*                # GlobalGrpcExceptionFilter, validation
src/shared/*                # config/logger
src/protogen/*              # generated (npm run proto:all)
prisma/                     # if Prisma-backed
```
Don't scaffold empty layers; right-size folder depth to the service.

## 3. Wire it up
- **Port:** assign the next free port; record it in root `CLAUDE.md`'s authoritative port table (the README table is stale).
- **Env:** add `development/envs/.env.<name>`.
- **Compose:** add a `build: { context: ../services/<name>-svc }` block to `development/docker-compose.aws.yml` (and `.dev.yml` if relevant). Build contexts point at `../services/<name>-svc` — never `../ticketbottle-*`.
- **Proto regen:** add the new consumer to the root `Makefile` `proto-go`/`proto-ts` target.
- **Clients:** if the gateway exposes it, register a gRPC client in `api-gateway/src/shared/microservices` and add a feature module.

## 4. Document
Add a service `CLAUDE.md` (agent guidance) and `README.md` (human run/build) following the existing concise style. Update root `CLAUDE.md` if you introduce a system-wide rule.
