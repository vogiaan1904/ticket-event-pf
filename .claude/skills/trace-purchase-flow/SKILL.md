---
name: trace-purchase-flow
description: Use when debugging or explaining the end-to-end ticket purchase in TicketBottle — the Waitroom → Order/Temporal → Inventory → Payment → Kafka → Order confirm chain. Gives the canonical sequence and the file/topic to look at for each hop.
---

# Tracing the purchase flow

The canonical chain (synchronous gRPC unless noted; Kafka hops marked):

1. **Waitroom admits a user.** `waitroom-svc/internal/service/queue_processor.go` — bounded-concurrency admission loop pops from the Redis sorted set, mints a short-lived JWT checkout token, publishes to `queue.ready` (**Kafka**). (The event and its `AllowWaitRoom` config were already validated via the Event gRPC client when the user joined the queue, in `waitroom_service.go`'s `JoinQueue` — not here.)
2. **Gateway → Order.** `api-gateway/src/modules/orders/` calls Order's gRPC `CreateOrder`. Before the saga starts, `order-svc/internal/order/service/order.go` claims a purchase slot keyed on the waiting-room session (or `user#<id>:event#<id>` when the event has none), so a retried or duplicate call is handed back the in-flight order instead of starting a second one.
3. **Order saga `CreateOrder`** (`order-svc/internal/workflows/create_order.go` + `internal/activities`): **reserve inventory** first (→ Inventory `Reserve`, a pessimistic row lock in `inventory-svc/internal/services/reservation.go`) → create order (DynamoDB) → create order items → **create payment intent** (→ Payment `CreatePaymentIntent`). Availability is not pre-checked: `Reserve` decides it under the lock, and inventory-svc's `CheckAvailability` RPC still exists but only backs a UI query now — no workflow calls it. Auto-compensates in reverse on any failure (delete order items → delete order → release the hold).
4. **Payment intent + webhook.** Payment gRPC service writes payment + an **outbox row** in one transaction (`payment-svc/src/modules/outbox/outbox.service.ts`). The **webhook** is an in-cluster workload (`payment-webhook`), not the gRPC service.
5. **Outbox → Kafka.** `payment-svc/outbox-relay/` is a long-lived relay: a dedicated `LISTEN` connection wakes on `NOTIFY`, a safety poll covers missed wakeups, and each drain claims rows with `FOR UPDATE SKIP LOCKED` inside one transaction (claim → publish → mark → commit), routing each row by event type onto `payment.completed`, `payment.failed`, or `payment.cancelled` (**Kafka**).
6. **Order confirm.** `order-svc/cmd/consumer` consumes `payment.completed` and triggers the Temporal **`ConfirmOrder`** workflow: confirm inventory (Inventory `Confirm`), mark order COMPLETED, publish `checkout.completed` (**Kafka**).
7. **Paid but unfulfillable → refund.** If inventory confirm fails (the hold expired and the stock was resold) or a payment event lands on an order already cancelled/timed out/failed, `ConfirmOrder` marks the order `REFUND_REQUIRED` instead of COMPLETED and publishes a `RefundRequiredEvent` on `order.refund_required` (**Kafka**) — see `markForRefund` in `order-svc/internal/workflows/confirm_order.go`. Nothing in the codebase consumes that topic yet, so an order stuck in `REFUND_REQUIRED` is a manual-reconciliation signal, not evidence of a lost message.
8. **Waitroom frees the slot.** Waitroom consumes `checkout.completed` and admits the next user.

## Debugging tips
- **Stuck order / not confirming?** The chart deploys no Temporal UI or Kafka UI. Inspect the workflow directly (`kubectl -n ticketbottle exec deploy/temporal -- tctl --address 127.0.0.1:7233 workflow describe -w ConfirmOrder:<order-code>`), check whether `payment.completed` carried the event (`kubectl -n ticketbottle exec redpanda-0 -- rpk topic consume payment.completed`), then the order consumer logs (`kubectl -n ticketbottle logs deploy/order-consumer`).
- **Oversold / reservation issues?** The invariant lives in `inventory-svc/internal/services/reservation.go` (`FOR UPDATE` inside a tx) + the `ReservationExpiryWorker` that auto-releases expired holds.
- **Payment not advancing?** The gRPC service only *writes* the outbox row (`payment-svc/src/modules/outbox/outbox.service.ts`) and carries no Kafka client at all. Publishing runs entirely in the `outbox-relay` workload (`kubectl -n ticketbottle logs deploy/outbox-relay`).
- **Paid order never completes?** Check its status for `REFUND_REQUIRED` before assuming the payment event was lost — see step 7.
- **Datastore:** Order is DynamoDB-only. Locally that is the `dynamodb` pod in the chart (image `amazon/dynamodb-local`); on the k3s and EKS targets it is the real table provisioned by `envs/foundation`.

Topics: `queue.ready`, `checkout.completed`, `checkout.failed`, `checkout.expired`, `payment.completed`, `payment.failed`, `payment.cancelled`, `order.refund_required`. Contracts: root `proto/`.
