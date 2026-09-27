# Order — reading guide

*Trust labels checked against the code on 2026-09-23.*

For the saga: `CreateOrder` and `ConfirmOrder`, compensation, the purchase slot, the
inventory hold window, `REFUND_REQUIRED`, the order consumer, and orders in DynamoDB.

## Read in this order

1. `services/order-svc/CLAUDE.md` — **current**: the two binaries, both workflows in
   outline, the DynamoDB single-table keys and pagination, and the order-svc-only
   error rule.
2. `.claude/skills/trace-purchase-flow/SKILL.md` — the whole purchase hop by hop,
   with the file and topic for each; where to look when an order is stuck.
3. `services/order-svc/docs/PURCHASE_SLOT.md` — **current**: one in-flight purchase
   per buyer; key, lifecycle, settle window, and the release every failed or
   finished `CreateOrder` must reach — read it before changing any failure path.
4. `services/order-svc/docs/RESERVATION_HOLD.md` — **current**: why the inventory
   hold outlives the payment window.
5. `services/order-svc/docs/SYSTEM.md` — background: activities, event schemas,
   the gRPC surface, config. Corrected against the code on 2026-09-24; verify
   anything it says about a workflow in the workflow file.

## By question

| Question | Owner | Verify in |
|---|---|---|
| The steps of a purchase and their compensation | service `CLAUDE.md`, *Temporal workflows* | `services/order-svc/internal/workflows/create_order.go`, `services/order-svc/internal/workflows/steps.go` |
| What happens after payment succeeds | same | `services/order-svc/internal/workflows/confirm_order.go` |
| Paid but not fulfillable — `REFUND_REQUIRED` | trace-purchase-flow skill, step 7; `docs/RUNBOOK.md`, *OrdersNeedingRefund* | `services/order-svc/internal/workflows/confirm_order.go` |
| The buyer's purchase slot: a retried `CreateOrder`, and its release when one fails | `services/order-svc/docs/PURCHASE_SLOT.md` | `services/order-svc/internal/order/purchase_slot.go`, `services/order-svc/internal/order/service/order.go` |
| Timeouts, hold length, retry policy | `services/order-svc/docs/RESERVATION_HOLD.md` | `services/order-svc/internal/workflows/shared.go`, `services/order-svc/internal/workflows/options.go` |
| How a domain error becomes a gRPC code | root `CLAUDE.md`, *Error taxonomy* | `services/order-svc/internal/order/delivery/grpc/errors.go` |
| What fails first as checkouts rise | root `CLAUDE.md`, register — **open** (Temporal shares the app's Postgres) | `deploy/helm/ticketbottle/templates/infra/temporal.yaml` |
| Saga alerts | `docs/RUNBOOK.md`, *SagaCompensationSpike* | `deploy/helm/ticketbottle/templates/apps/prometheusrule.yaml` |

## Code entry points

| To see | Open |
|---|---|
| The API binary: gRPC server and the create-order worker | `services/order-svc/cmd/api/main.go` |
| The consumer binary: Kafka and the confirm-order worker | `services/order-svc/cmd/consumer/main.go` |
| Task queues and workers | `services/order-svc/internal/infra/temporal/workers.go` |
| Calls to the other services | `services/order-svc/internal/activities/` |
| Payment events in | `services/order-svc/internal/order/delivery/kafka/consumer/handler.go` |
| Orders in DynamoDB | `services/order-svc/internal/order/repository/order.go` |

## Edges

- **Serves** `proto/order.proto`, to the gateway — `services/api-gateway/src/modules/orders/orders.service.ts`.
- **Calls** event, inventory and payment over gRPC, one activity file each in
  `services/order-svc/internal/activities/`.
- **Kafka** — consumes payment events, produces checkout and refund events; names in
  `services/order-svc/internal/order/delivery/kafka/constants.go`.
- **Stores** — DynamoDB for orders; Temporal for workflow state.
- **Deployed by** `deploy/helm/ticketbottle/templates/apps/order.yaml` — both
  binaries.

## Don't trust for current behaviour

- `services/order-svc/docs/SYSTEM.md` where it is more specific than the service
  `CLAUDE.md` about a timeout or a retry — `services/order-svc/internal/workflows/`
  is the owner of those numbers.
- The gateway's generated order types — see [api-gateway.md](api-gateway.md).
