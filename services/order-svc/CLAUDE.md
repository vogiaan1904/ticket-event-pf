# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **Order** service for TicketBottle V2 — the saga orchestrator. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`. **Keep this file updated when you change a rule or a system invariant.**

## Role

gRPC service (port **50054**) that coordinates the distributed purchase transaction using **Temporal** workflows. It calls Event, Inventory, and Payment over gRPC and reacts to Kafka events. It ships as **two binaries**:
- `cmd/api` — gRPC server + Temporal client (starts workflows).
- `cmd/consumer` — Kafka consumer that triggers `ConfirmOrder` on `PAYMENT_COMPLETED`.

### Temporal workflows (`internal/workflows`, activities in `internal/activities`)
- `CreateOrder` — check availability → reserve inventory → create order → create payment intent; **auto-compensates** on any failure (release tickets → delete order items → delete order). Workflow state tracks how far it got so rollback is exact.
- `ConfirmOrder` — on payment success: confirm inventory, mark order COMPLETED, publish `CHECKOUT_COMPLETED`.

## Datastore: DynamoDB only
This service is **DynamoDB-only** (`dynamodbav` tags, `internal/infra/dynamodb`). The MongoDB driver was removed — `internal/infra/mongo/` no longer exists, and there is no `legacy/mongodb` branch in this monorepo.

For local DynamoDB, run `docker compose -f docker-compose.dev.yml up -d` — this brings up `amazon/dynamodb-local` (container `ticketbottle-order-dynamodb`, port 8000), the same image the Helm chart uses for the same job. The repository and activity integration tests (`internal/order/repository`, `internal/activities`) create the table on first use via `internal/testutil/dynamotest`, skip locally when the datastore is unreachable, and **fail** when `CI` is set — a suite that skips itself reports PASS having asserted nothing.

## Commands

```bash
make run-api        # run the gRPC API binary
make run-consumer   # run the Kafka consumer binary
make protoc         # regenerate protobuf
make update-proto   # regenerate gRPC stubs from the root proto/
go test ./...       # tests
go build ./...
```

## Code style (enforced conventions)

1. **Logging:** always use the custom zap wrapper with the `f`-suffixed, ctx-first methods, e.g. `s.l.Errorf(ctx, "failed to start create order workflow: %v", err)`.
2. **Errors:** do **not** return errors via `fmt.Errorf("...")` — declare an error `var` and return that instead. (This rule is Order-specific within the repo.)

## DynamoDB (`main` branch) — single-table design

- Table: `ticketbottle-orders`. Primary key `PK` (partition) + `SK` (sort). `GSI1` (`GSI1PK`/`GSI1SK`) queries by UserID; `GSI2` (`GSI2PK`/`GSI2SK`) queries by EventID.
- **Order Code** is the primary business identifier (not a Mongo ObjectID).
- Key patterns:
  - Order: `PK=ORDER#<code>`, `SK=ORDER#<code>`
  - OrderItem: `PK=ORDER#<orderCode>`, `SK=ITEM#<itemId>`
  - GSI1: `GSI1PK=USER#<userId>`, `GSI1SK=ORDER#<createdAt>#<code>`
  - GSI2: `GSI2PK=EVENT#<eventId>`, `GSI2SK=ORDER#<createdAt>#<code>`
- **Pagination:** cursor-based (not page-based). Cursor = base64-encoded DynamoDB `LastEvaluatedKey`. Responses carry `Count, PageSize, NextCursor, HasMore`.
- Env: `DYNAMODB_TABLE_NAME` (default `ticketbottle-orders`), `AWS_REGION` (default `us-east-1`), `DYNAMODB_ENDPOINT` (empty for real AWS, set for a local DynamoDB such as dynamodb-local).

## Layout

- `internal/order/{delivery,service,repository}` — feature slice; `internal/{workflows,activities}` — Temporal; `internal/infra/{dynamodb,kafka,temporal}` — adapters; `internal/models`, `internal/interceptors`.
- `pkg/` — shared `temporal`, `kafka`, `dynamodb`, `paginator`, `logger`, `errors`, `grpc`, `jwt`, `redis`, `response`, `util`. See `docs/SYSTEM.md` for a deeper design write-up.
