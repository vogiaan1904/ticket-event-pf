# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

TicketBottle V2 is a polyglot microservices platform for high-traffic ticket sales (virtual queue → atomic inventory → saga-orchestrated order → multi-provider payment). It is a **single monorepo with one git history** — every service lives under `services/<name>-svc/` (the `api-gateway` dir has no `-svc` suffix). The `.proto` contracts are shared from the root `proto/` directory.

Each service has its own `CLAUDE.md` with service-specific detail — read that file when working inside a service.

See `README.md` for the full prose architecture and the end-to-end purchase data flow. `aws/ARC.md` and `aws/PLAN.md` cover the AWS/Lambda deployment direction. `REVIEW.md` (repo root) is a standing architecture review with the current cleanup backlog.

## Services & ports

Ports below are the **authoritative** values (from each service's config/`main`). The port table in the root `README.md` is stale — do not trust it.

| Service | Dir | Lang/Framework | Port | Protocol | Datastore |
|---------|-----|----------------|------|----------|-----------|
| API Gateway | `services/api-gateway` | TS / NestJS | 3000 | HTTP/REST + Swagger | — (gRPC client to all services) |
| User | `services/user-svc` | TS / NestJS | 50052 | gRPC | PostgreSQL (Prisma) |
| Event | `services/event-svc` | TS / NestJS | 50053 | gRPC | PostgreSQL (Prisma) |
| Order | `services/order-svc` | Go | 50054 | gRPC | DynamoDB |
| Payment | `services/payment-svc` | TS / NestJS | 50055 | gRPC | PostgreSQL (Prisma) |
| Waitroom | `services/waitroom-svc` | Go | 50056 | gRPC | Redis |
| Inventory | `services/inventory-svc` | Go | 50057 | gRPC | PostgreSQL (GORM) |

`proto/` is **not a service** — it is the shared `.proto` contract source (see `proto/CLAUDE.md`).

## Architecture in one paragraph

The **API Gateway** is the only HTTP entry point; everything behind it is gRPC. The **Order** service is the saga orchestrator: it drives a **Temporal** workflow that calls Event → Inventory → Payment synchronously over gRPC, and compensates on failure. Cross-service eventual consistency flows over **Kafka** (topics like `payment-events`, `order-events`, `queue-events`). Canonical chain: Waitroom admits a user → Gateway calls Order → Temporal `CreateOrder` reserves inventory + creates a payment intent → payment webhook → Payment writes an outbox row → outbox is published to Kafka → Order's `ConfirmOrder` workflow confirms inventory and completes the order → Waitroom frees the checkout slot.

## Communication patterns (where to look when tracing a flow)

- **Synchronous gRPC** — request/response needing an immediate answer (Order→Inventory reserve, Order→Payment intent, Gateway→everything). Contracts live in `proto/`.
- **Asynchronous Kafka** — event notifications / eventual consistency (Payment→Order, Order→Waitroom).
- **Temporal workflows** (Order service only) — long-running, stateful, auto-compensating saga steps.

## Local development

Compose files and the Makefile live in `development/`. Build contexts point at `../services/<name>-svc`.

```bash
cd development
make up-aws      # DynamoDB mode (LocalStack) — the supported path; order-svc is DynamoDB-only
make down-aws    # stop AWS mode
make status      # container status
make clean       # remove containers + volumes
make up          # legacy MongoDB mode — see note below
```

> **`make up` (MongoDB mode) is legacy and currently non-functional.** `order-svc` on `main` is DynamoDB-only — the MongoDB driver/code has been removed (`internal/infra/mongo/` is empty). Use `make up-aws`. The `docker-compose.dev.yml` MongoDB path and the `mongo-order` container are kept only for history and should be removed or revived deliberately.

Per-service env files live in `development/envs/.env.*`. Infra ports: Kafka 9092 (UI 8090), Temporal 7233 (UI 8080), Redis 6379 (waitroom) / 6380 (auth), LocalStack 4566; Postgres — Payment 5433, Event 5434, Inventory 5435, User 5436.

## Proto contracts & generation

There is **one source of truth: the root `proto/` directory.** Edit contracts there, then regenerate in every consumer. Generated code is committed (TS under `src/protogen/`, Go under `pkg/grpc/` or `protogen/`), so a fresh checkout builds without regenerating.

- **TS services** (`api-gateway`, `event`, `user`, `payment`): `npm run update:proto` — syncs the contracts into the service's `src/protos/` and runs `proto:all` (`protoc` + `ts-proto`, `-I=../../proto`) into `src/protogen/`. **Why `src/protos/` exists:** the NestJS gRPC transport loads `.proto` at *runtime* (`protoPath`) and `nest-cli.json` copies `src/protos/**` into `dist` as build assets — so each TS service needs a local copy. It is a **generated, synced-from-root** artifact: never hand-edit it, edit `proto/` and re-run `update:proto`.
- **Go services** (`order`, `inventory`, `waitroom`): `make protoc-all` — runs `protoc` with the Go plugins against `../../proto` into `pkg/grpc/<svc>/` (waitroom: `protogen/`). Go uses **compiled stubs only** (no runtime `.proto`), so Go services keep **no** local `.proto` copy.

Do **not** reintroduce the old redundant copies (`protos/`, `protos-submodule/`) — those were dead submodule remnants. The only legitimate local copy is each TS service's `src/protos/`, kept in sync by `update:proto`. (Target state is `buf generate` from a single root module, which removes even the TS copy.)

## Conventions that span services

- **Go services** (`order`, `inventory`, `waitroom`) share a layout: `cmd/<binary>/main.go` → `internal/{delivery,service(s),repository,models}` → shared `pkg/` (logger, errors, grpc, response, util). Logging uses the custom zap wrapper with `f`-suffixed, ctx-first methods: `l.Errorf(ctx, "...", err)`, `l.Infof(ctx, "...")`. (The "use error vars, never `fmt.Errorf`" rule is **Order-specific** — see `services/order-svc/CLAUDE.md`; the other Go services use `fmt.Errorf` freely.)
- **TS services** (`api-gateway`, `event`, `user`, `payment`) are NestJS. The gateway boots an HTTP app (`NestFactory.create`); the others boot gRPC microservices (`NestFactory.createMicroservice`, `Transport.GRPC`). Prisma services use `prisma migrate dev` / migrations under `prisma/`.
  - **Canonical TS layout (target convention — apply when touching a service):**
    `src/main.ts` → `src/modules/<feature>/` (controller + service + module + `dto/` + `repository.ts`) → `src/common/*` (filters, guards, interceptors, decorators) → `src/shared/*` (cross-cutting helpers) → `src/protogen/*` (generated, never hand-edited).
    - One `dto/` per module — do **not** split into parallel `dtos/req`+`dtos/resp`+`controllers/grpc/dtos` trees.
    - Prefer the generated proto types/enums directly; avoid redefining domain enums and hand-writing enum↔proto mappers.
    - Don't scaffold empty layers. A small service (e.g. `user-svc`) should not carry the same folder depth as the gateway. Collapse single-file `common/`/`shared/` folders into flat files.
  - The three TS services currently use **three different** module/DTO conventions — converging them on the above is tracked in `REVIEW.md` (P2).

When you change a system-wide rule or invariant, update this file.
