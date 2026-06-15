# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

TicketBottle V2 is a polyglot microservices platform for high-traffic ticket sales (virtual queue → atomic inventory → saga-orchestrated order → multi-provider payment). This top-level repo is an **umbrella**: each `ticketbottle-*` directory is an independently-versioned service with its own git history (the `.gitmodules` wiring is currently removed, so treat them as sibling checkouts). Each service has its own `CLAUDE.md` with service-specific detail — read that file when working inside a service.

See `README.md` for the full prose architecture and the end-to-end purchase data flow. `aws/ARC.md` and `aws/PLAN.md` cover the AWS/Lambda deployment direction.

## Services & ports

Ports below are the **authoritative** values (from each service's config/`main`). The port table in the root `README.md` is stale — do not trust it.

| Service | Dir | Lang/Framework | Port | Protocol | Datastore |
|---------|-----|----------------|------|----------|-----------|
| API Gateway | `ticketbottle-api-gateway` | TS / NestJS | 3000 | HTTP/REST + Swagger | — (gRPC client to all services) |
| User | `ticketbottle-user` | TS / NestJS | 50052 | gRPC | PostgreSQL (Prisma) |
| Event | `ticketbottle-event` | TS / NestJS | 50053 | gRPC | PostgreSQL (Prisma) |
| Order | `ticketbottle-order` | Go | 50054 | gRPC | DynamoDB (`main`) / MongoDB (`legacy/mongodb`) |
| Payment | `ticketbottle-payment` | TS / NestJS | 50055 | gRPC | PostgreSQL (Prisma) |
| Waitroom | `ticketbottle-waitroom` | Go | 50056 | gRPC | Redis |
| Inventory | `ticketbottle-inventory` | Go | 50057 | gRPC | PostgreSQL (GORM) |

`ticketbottle-proto` is **not a service** — it is the shared `.proto` contract source (see its own `CLAUDE.md`).

## Architecture in one paragraph

The **API Gateway** is the only HTTP entry point; everything behind it is gRPC. The **Order** service is the saga orchestrator: it drives a **Temporal** workflow that calls Event → Inventory → Payment synchronously over gRPC, and compensates on failure. Cross-service eventual consistency flows over **Kafka** (topics like `payment-events`, `order-events`, `queue-events`). Canonical chain: Waitroom admits a user → Gateway calls Order → Temporal `CreateOrder` reserves inventory + creates a payment intent → payment webhook → Payment writes an outbox row → outbox is published to Kafka → Order's `ConfirmOrder` workflow confirms inventory and completes the order → Waitroom frees the checkout slot.

## Communication patterns (where to look when tracing a flow)

- **Synchronous gRPC** — request/response needing an immediate answer (Order→Inventory reserve, Order→Payment intent, Gateway→everything). Contracts live in `ticketbottle-proto`.
- **Asynchronous Kafka** — event notifications / eventual consistency (Payment→Order, Order→Waitroom).
- **Temporal workflows** (Order service only) — long-running, stateful, auto-compensating saga steps.

## Local development

Compose files and Makefile live in `development/`. There are **two mutually exclusive modes**, and they require different Order-service images:

```bash
cd development
make up          # MongoDB mode  — needs Order built from the legacy/mongodb branch
make up-aws      # DynamoDB mode — needs Order built from main (LocalStack provides DynamoDB)
make down        # stop MongoDB mode
make status      # container status
make clean       # remove containers + volumes
```

**Order-service gotcha:** the Order service has genuinely different code on two branches — `legacy/mongodb` (MongoDB driver, `bson` tags) and `main` (DynamoDB, `dynamodbav` tags). You must check out and build the matching branch before `make up` / `make up-aws`, or the container will fail to talk to its datastore. See `development/README.md`.

Per-service env files live in `development/envs/.env.*`. Infra ports: Kafka 9092 (UI 8090), Temporal 7233 (UI 8080), Redis 6379 (waitroom) / 6380 (auth), LocalStack 4566; Postgres — Payment 5433, Event 5434, Inventory 5435, User 5436; Mongo 27017.

## Conventions that span services

- **Go services** (`order`, `inventory`, `waitroom`) share a layout: `cmd/<binary>/main.go` → `internal/{delivery,service(s),repository,models}` → shared `pkg/` (logger, errors, grpc, response, util). Logging always uses the custom zap wrapper with `f`-suffixed, ctx-first methods: `l.Errorf(ctx, "...", err)`, `l.Infof(ctx, "...")`. (The "use error vars, never `fmt.Errorf`" rule is **Order-specific** — see `ticketbottle-order/CLAUDE.md`; the other Go services use `fmt.Errorf` freely.)
- **TS services** (`api-gateway`, `event`, `user`, `payment`) are NestJS. The gateway boots an HTTP app (`NestFactory.create`); the others boot gRPC microservices (`NestFactory.createMicroservice`, `Transport.GRPC`). Layout: `src/modules/*` (feature modules), `src/common/*` (filters/guards/interceptors), `src/shared/*`, `src/protogen/*` (generated). Prisma services use `prisma migrate dev` / migrations under `prisma/`.
- **Proto generation** differs by stack: Go services vendor the contracts via a `protos-submodule/` dir and run `make protoc`/`make update-proto`; TS services copy `.proto` into `src/protos` (or `protos/`) and generate TS via the `proto:*` npm scripts. Edit contracts in `ticketbottle-proto`, then regenerate in each consumer.

When you change a system-wide rule or invariant, update this file.
