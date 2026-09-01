# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

TicketBottle V2 is a polyglot microservices platform for high-traffic ticket sales (virtual queue → atomic inventory → saga-orchestrated order → multi-provider payment). It is a **single monorepo with one git history** — every service lives under `services/<name>-svc/` (the `api-gateway` dir has no `-svc` suffix). The `.proto` contracts are shared from the root `proto/` directory.

Each service has its own `CLAUDE.md` with service-specific detail — read that file when working inside a service.

See `README.md` for the architecture overview and the end-to-end purchase data flow, and `docs/ARCHITECTURE.md` for the longer design walkthrough of each decision. `deploy/README.md` covers the Helm chart, its per-target values overlays, and the Terraform under `deploy/terraform/`.

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

`proto/` is **not a service** — it is the shared `.proto` contract source (see "Proto contracts & generation" below).

## Architecture in one paragraph

The **API Gateway** is the only HTTP entry point; everything behind it is gRPC. The **Order** service is the saga orchestrator: it drives a **Temporal** workflow that calls Event → Inventory → Payment synchronously over gRPC, and compensates on failure. Cross-service eventual consistency flows over **Kafka** (topics like `payment-events`, `order-events`, `queue-events`). Canonical chain: Waitroom admits a user → Gateway calls Order → Temporal `CreateOrder` reserves inventory + creates a payment intent → payment webhook → Payment writes an outbox row → outbox is published to Kafka → Order's `ConfirmOrder` workflow confirms inventory and completes the order → Waitroom frees the checkout slot.

## Communication patterns (where to look when tracing a flow)

- **Synchronous gRPC** — request/response needing an immediate answer (Order→Inventory reserve, Order→Payment intent, Gateway→everything). Contracts live in `proto/`.
- **Asynchronous Kafka** — event notifications / eventual consistency (Payment→Order, Order→Waitroom).
- **Temporal workflows** (Order service only) — long-running, stateful, auto-compensating saga steps.

## Local development

Local **full-stack** dev runs on **kind + Helm** (the `deploy/` tree). The old *full-stack* Docker Compose setup under `development/` was **retired** — `kind` is the single **full-stack** local path. For **single-service inner-loop** work, use the per-service `docker-compose.dev.yml` (spins up only that service's datastore; run the service natively with hot-reload, then `docker compose down -v`) — this is the sanctioned, disk-light inner-loop tool. Full-stack operations live in `deploy/Makefile`:

```bash
make -C deploy cluster-up   # create the kind cluster
make -C deploy infra-up     # infra tier (Postgres / Redis / Redpanda / Temporal / DynamoDB-local)
make -C deploy apps-up      # build + kind-load the app images, deploy the app tier
make -C deploy apps-deploy  # chart-only change: helm upgrade, no image rebuild
make -C deploy gate1        # full purchase-flow acceptance test
make -C deploy cluster-down # tear it all down
```

Per-service config is baked into the chart's ConfigMaps (`deploy/helm/ticketbottle/templates/apps/config.yaml`), **not** env files. The API Gateway is reachable at `localhost:3000` (kind NodePort → 30000).

**Cloud targets.** The same chart deploys to AWS through values overlays (`values-k3s.yaml`, `values-eks.yaml`) plus the Terraform under `deploy/terraform/`; images are built in CI and pushed to ECR. `deploy/localstack/` is a retired local AWS simulation kept only for reference — do not build on it.

## Proto contracts & generation

There is **one source of truth: the root `proto/` directory.** Edit contracts there, then regenerate in every consumer. Generated code is committed (TS under `src/protogen/`, Go under `pkg/grpc/` or `protogen/`), so a fresh checkout builds without regenerating.

- **TS services** (`api-gateway`, `event`, `user`, `payment`): `npm run update:proto` — syncs the contracts into the service's `src/protos/` and runs `proto:all` (`protoc` + `ts-proto`, `-I=../../proto`) into `src/protogen/`. **Why `src/protos/` exists:** the NestJS gRPC transport loads `.proto` at *runtime* (`protoPath`) and `nest-cli.json` copies `src/protos/**` into `dist` as build assets — so each TS service needs a local copy. It is a **generated, synced-from-root** artifact: never hand-edit it, edit `proto/` and re-run `update:proto`.
- **Go services** (`order`, `inventory`, `waitroom`): `make protoc-all` — runs `protoc` with the Go plugins against `../../proto` into `pkg/grpc/<svc>/` (waitroom: `protogen/`). Go uses **compiled stubs only** (no runtime `.proto`), so Go services keep **no** local `.proto` copy.

Do **not** reintroduce the old redundant copies (`protos/`, `protos-submodule/`) — those were dead submodule remnants. The only legitimate local copy is each TS service's `src/protos/`, kept in sync by `update:proto`. (Target state is `buf generate` from a single root module, which removes even the TS copy.)

## Error taxonomy (binding for every service)

A failure crosses two boundaries: domain error → gRPC code → HTTP status. The
gRPC code is the contract; the API Gateway maps it to HTTP in
`common/filters/global-exception.filter.ts` and nowhere else.

| gRPC code | HTTP | Means | Client should |
|---|---|---|---|
| `INVALID_ARGUMENT` | 400 | The request is malformed | Fix the request |
| `UNAUTHENTICATED` | 401 | Missing or invalid credentials | Re-authenticate |
| `PERMISSION_DENIED` | 403 | Authenticated but not allowed | Stop |
| `NOT_FOUND` | 404 | The entity does not exist | Stop or refetch |
| `ALREADY_EXISTS` | 409 | Duplicate create | Treat as success, or refetch |
| `FAILED_PRECONDITION` | 409 | Valid request, world is in the wrong state | Refetch; do not retry identically |
| `DEADLINE_EXCEEDED` | 504 | We did not answer in time | Retry |
| `UNAVAILABLE` | 503 | A dependency is down | Retry with backoff |
| `INTERNAL` | 500 | **We have a bug** | Retry later; page someone |

**The governing rule: `INTERNAL` means we have a bug.** If a business outcome can
produce it, the mapping is wrong. Sold out, sale closed, queue full and wrong-state
are all `FAILED_PRECONDITION` — a buyer losing a race is not a server fault.

`RESOURCE_EXHAUSTED` is deliberately unused: its canonical HTTP mapping is 429,
which tells a buyer they were rate-limited when the truth is the show sold out.

**The code is required, not defaulted.** Go services take it as the first argument
of `pkgErrors.NewGRPCError(codes.X, "ID", "message")`, so omitting it does not
compile; `response.GrpcError` has no fallback. TS services carry it as the third
element of the `ErrorCode` tuple — `[message, httpStatus, grpcCode]` — so a missing
one fails `tsc`. Neither side has a default: a silent fallback is how an error
ends up with the wrong class.

## Conventions that span services

- **Go services** (`order`, `inventory`, `waitroom`) share a layout: `cmd/<binary>/main.go` → `internal/{delivery,service(s),repository,models}` → shared `pkg/` (logger, errors, grpc, response, util). Logging uses the custom zap wrapper with `f`-suffixed, ctx-first methods: `l.Errorf(ctx, "...", err)`, `l.Infof(ctx, "...")`. (The "use error vars, never `fmt.Errorf`" rule is **Order-specific** — see `services/order-svc/CLAUDE.md`; the other Go services use `fmt.Errorf` freely.)
- **TS services** (`api-gateway`, `event`, `user`, `payment`) are NestJS. The gateway boots an HTTP app (`NestFactory.create`); the others boot gRPC microservices (`NestFactory.createMicroservice`, `Transport.GRPC`). Prisma services use `prisma migrate dev` / migrations under `prisma/`.
  - **Canonical TS layout (target convention — apply when touching a service):**
    `src/main.ts` → `src/modules/<feature>/` (controller + service + module + `dto/` + `repository.ts`) → `src/common/*` (filters, guards, interceptors, decorators) → `src/shared/*` (cross-cutting helpers) → `src/protogen/*` (generated, never hand-edited).
    - One `dto/` per module — do **not** split into parallel `dtos/req`+`dtos/resp`+`controllers/grpc/dtos` trees.
    - Prefer the generated proto types/enums directly; avoid redefining domain enums and hand-writing enum↔proto mappers.
    - Don't scaffold empty layers. A small service (e.g. `user-svc`) should not carry the same folder depth as the gateway. Collapse single-file `common/`/`shared/` folders into flat files.
  - The three TS services currently use **three different** module/DTO conventions. `event-svc` is the converged reference implementation; bring the others onto it when you touch them.

When you change a system-wide rule or invariant, update this file.
