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

**NestJS** — follow `docs/design/ts-layout.md`, the domain-service or small-service
archetype. No existing TS service is converged, so do not copy one:
```
src/main.ts                   # NestFactory.createMicroservice, Transport.GRPC
src/modules/<feature>/        # <feature>.controller.ts, .service.ts, .module.ts, dto/
                              # + <feature>.types.ts, repository/, entities/ for a domain service
src/infra/database/prisma/    # if Prisma-backed
src/common/*                  # exception filter, validation
src/shared/*                  # config, logger
src/protogen/*                # generated (npm run proto:all)
```
Don't scaffold empty layers; right-size folder depth to the service.

## 3. Wire it up
- **Port:** assign the next free port; record it in root `CLAUDE.md`'s authoritative port table and in the README's copy for human readers.
- **Config:** add a `<name>-config` ConfigMap block to `deploy/helm/ticketbottle/templates/apps/config.yaml`.
- **Chart:** add the image to the matrix in `.github/workflows/build-push-ecr.yml` (and its ECR repository to `deploy/terraform/envs/foundation/main.tf`) and a Deployment via the `tb.appService` template (`deploy/helm/ticketbottle/templates/apps/`). Build contexts point at `services/<name>-svc` — never `../ticketbottle-*`.
- **Proto regen:** add the new consumer to the root `Makefile` `proto-go`/`proto-ts` target.
- **Clients:** if the gateway exposes it, register a gRPC client in `api-gateway/src/shared/microservices` and add a feature module.

## 4. Document
Add a service `CLAUDE.md` (agent guidance) and `README.md` (human run/build) following the existing concise style. Update root `CLAUDE.md` if you introduce a system-wide rule.
