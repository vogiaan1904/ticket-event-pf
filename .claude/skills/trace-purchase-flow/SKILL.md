---
name: trace-purchase-flow
description: Use when debugging or explaining the end-to-end ticket purchase in TicketBottle — the Waitroom → Order/Temporal → Inventory → Payment → Kafka → Order confirm chain. Gives the canonical sequence and the file/topic to look at for each hop.
---

# Tracing the purchase flow

The canonical chain (synchronous gRPC unless noted; Kafka hops marked):

1. **Waitroom admits a user.** `waitroom-svc/internal/service/queue_processor.go` — bounded-concurrency admission loop pops from the Redis sorted set, mints a short-lived JWT checkout token, publishes `QUEUE_READY` (**Kafka**). Validates the event via the Event gRPC client first.
2. **Gateway → Order.** `api-gateway/src/modules/orders/` calls Order's gRPC `CreateOrder`.
3. **Order saga `CreateOrder`** (`order-svc/internal/workflows` + `internal/activities`): check availability → **reserve inventory** (→ Inventory `Reserve`, pessimistic `SELECT ... FOR UPDATE` in `inventory-svc/internal/services/reservation.go`) → create order (DynamoDB) → **create payment intent** (→ Payment `createPaymentIntent`). Auto-compensates in reverse on any failure (release tickets → delete items → delete order).
4. **Payment intent + webhook.** Payment gRPC service writes payment + an **outbox row** in one transaction (`payment-svc/src/modules/outbox/outbox.service.ts`). The **webhook** (ZaloPay/PayOS) and outbox→Kafka publishing run in the **Lambdas** (`payment-svc/lambdas/`), not the gRPC service.
5. **Outbox → Kafka.** `outbox-processor` Lambda batch-publishes pending rows to the `payment-events` topic (**Kafka**).
6. **Order confirm.** `order-svc/cmd/consumer` consumes `PAYMENT_COMPLETED` and triggers the Temporal **`ConfirmOrder`** workflow: confirm inventory (Inventory `Confirm`), mark order COMPLETED, publish `CHECKOUT_COMPLETED` (**Kafka**).
7. **Waitroom frees the slot.** Waitroom consumes `CHECKOUT_COMPLETED` and admits the next user.

## Debugging tips
- **Stuck order / not confirming?** Check Temporal UI (`localhost:8080`) for the workflow, then whether `payment-events` carried `PAYMENT_COMPLETED` (Kafka UI `localhost:8090`), then the order consumer logs (`kubectl -n ticketbottle logs deploy/order-consumer`).
- **Oversold / reservation issues?** The invariant lives in `inventory-svc/internal/services/reservation.go` (`FOR UPDATE` inside a tx) + the `ReservationExpiryWorker` that auto-releases expired holds.
- **Payment not advancing?** The gRPC service only *writes* the outbox; publishing is the `outbox-processor` Lambda. Editing `src/infra/messaging/kafka/` in payment-svc does **not** change live behavior.
- **Datastore:** Order is DynamoDB-only (`make up-aws` / LocalStack). The `make up` MongoDB mode is legacy/non-functional.

Topics: `payment-events`, `order-events`, `queue-events`. Contracts: root `proto/`.
