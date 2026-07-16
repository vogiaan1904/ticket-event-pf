# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **Payment** service for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role & current shape (post-Lambda split)

The live service is a NestJS **gRPC microservice** (port **50055**, PostgreSQL via Prisma) called by the Order service to create/cancel payment intents. It uses the **Outbox pattern**: payment status changes and their domain events are written to the outbox table in the *same* DB transaction (`src/modules/outbox/outbox.service.ts`).

What the gRPC service **no longer does** (verified in current code — these moved to AWS Lambda under `lambdas/`):
- ❌ Webhook handling (`OutboxPublisherService` and the HTTP webhook endpoint were removed).
- ❌ Publishing outbox rows to Kafka.
- ❌ Scheduled jobs (`@nestjs/schedule` is no longer wired in).

So: **the gRPC service only writes events to the outbox; the Lambdas read and publish them.** A `src/infra/messaging/kafka/` module still exists in the tree but is not driven by the live service — don't assume editing it changes runtime behavior.

### The Lambdas (`lambdas/`)
- `payment-webhook-handler` — receives ZaloPay/PayOS webhooks (HMAC/SDK signature verification), updates payment + writes an outbox row. Triggered by API Gateway.
- `outbox-processor` — batch-publishes pending outbox rows to Kafka with retry. EventBridge schedule (~1 min).
- `outbox-cleanup` — deletes old published rows / flags failed ones past max retries. EventBridge schedule (daily).
- Shared `common` layer (Prisma + Kafka singletons, logger, types) and a `dependencies` layer; deploy via SAM (`template.yaml`).

VNPay is a planned third provider (placeholder); ZaloPay and PayOS are implemented.

## Commands

```bash
# Main gRPC service
npm install
npm run start:dev      # watch mode
npm run start:prod
npm run build
npm run lint           # eslint --fix
npm run test           # jest  (npm run test -- <path> for one file)
npm run proto:all      # regenerate gRPC stubs into src/protogen

# Lambdas
cd lambdas && npm install && npm test
npm run build:layers   # build Lambda layers + zips
sam build && sam deploy --config-env dev --guided
```

## Layout

- `src/modules/payment/` — `createPaymentIntent` / `cancelPayment` gRPC controllers, service, repository, DTOs.
- `src/modules/outbox/` — `OutboxService` (save-event-in-transaction), enums, event types.
- `prisma/` — schema/migrations. `src/protogen/` generated from `../../proto/payment.proto`.

## Conventions

- **Cost-aware logging (Lambdas especially):** log one line per critical business event (`Payment completed`, `Batch processed`) and per error/warn; do **not** log per-step, per-loop-iteration, or "started/validated/completed" noise — use summary logs after batches. Use structured context objects (`{ paymentId, orderCode, requestId }`), never string interpolation. Never log amounts, provider transaction IDs, tokens, signatures, or PII; payment UUID and order code are safe.
- Keep payment status updates and outbox writes atomic (single transaction) — that invariant is the whole point of the outbox here.
