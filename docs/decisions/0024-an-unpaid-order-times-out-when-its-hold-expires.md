# 0024 — An unpaid order times out when its hold expires

**Date:** 2026-09-30
**Status:** accepted
**Arc:** admission-sizing — [design](../design/admission-sizing.md#when-a-checkout-is-abandoned), [plan](../plans/2026-09-30-an-abandoned-checkout-expires.md)
**Where it lives:** `services/order-svc/internal/workflows/expire_order.go`, `services/order-svc/internal/order/service/order.go` (`Create`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Who notices a checkout nobody pays for, and when? | order-svc, from a Temporal workflow delayed by the hold's length, which times the order out and publishes `checkout.expired` | The chair's TTL following the hold; a payment-svc sweeper; nothing, as today | One more Temporal workflow per order, and one start call on the checkout path, both unmeasured |

## Context

A buyer who takes a hold and never pays is noticed by nothing that tells anyone
else:
- inventory's expiry worker releases the hold at about 9 minutes and tells no one;
- the waitroom frees the chair at 15 minutes, from the slot's own TTL;
- the order stays `PENDING` for ever.

With no waiting room, the buyer is locked out of the event for 30 days. The
purchase slot is keyed by buyer and event, and each retry resumes the `PENDING`
order and its dead payment link.

The waitroom already consumes `checkout.expired`, and `ConfirmOrder` already has a
branch for a payment on a `TIMEOUT` order. Nothing produces either.

## Options

**Who runs the clock.**
- *order-svc, with a delayed Temporal workflow at the hold's expiry* (recommended).
  order-svc sets the hold's length and owns the checkout's statuses, so the order,
  the purchase slot and the chair are fixed together. The timer survives restarts.
- *The chair's TTL follows the hold.* order-svc publishes the hold's expiry when the
  checkout starts, and the waitroom shortens the chair to match. No timer anywhere,
  but the order stays `PENDING` and the lockout stays.
- *A payment-svc sweeper* turns overdue intents into `payment.failed`. It reuses the
  failure path, but calls an abandoned checkout a failed payment, and adds a
  periodic job that must be safe across replicas.

**What a payment that lands after the timeout gets.**
- *Confirmed if inventory can re-acquire the ticket; refunded only if it cannot*
  (recommended). This keeps the backstop in
  `services/order-svc/docs/RESERVATION_HOLD.md`, for a payment made inside the
  provider's window whose confirmation ran long.
- *Always refunded*, as `ConfirmOrder`'s switch does today. Simpler, but a buyer who
  paid in time loses a seat that is still there.

**What the buyer sees for a timed-out order.**
- *`CANCELED`* (recommended). No contract change, and every gateway understands it.
- *A new `EXPIRED` status.* That needs `proto/order.proto` and the gateway's
  `order.pb.ts`, which is stale; regenerating it breaks `src/modules/orders/`.

## Decision

The architect was given the three calls, each with a recommended option and its
alternatives, their costs as above, and the plan. The options were not lettered; the
answer format offered was "1 A, 2 A, 3 A", with A as the recommended option. They
answered: "for the decision, 1 A, 2 A, 3 A" (2026-09-30). That means:
1. order-svc runs the clock, with a delayed `ExpireOrder` at the hold's expiry;
2. a payment on a `TIMEOUT` order is confirmed if inventory can re-acquire the
   ticket, and refunded if not;
3. a `TIMEOUT` order reads `CANCELED` on the wire.

## Outcome

Built in `4d66ae6`, `5fbf456`, `8e7fc30` and `df53d87`; the acceptance run in `4c17427`.
The review then found that the clock's start shared the request's deadline, so a saga
finishing near `createTimeout`, or a caller hanging up as it returned, left a live
order with no clock. `ab36c06` starts it on its own 5s deadline.

Each test below failed before its code existed, and fails again when the code it guards
is broken:
- `TestExpireIfPending_LeavesAPaidOrderAlone`. Without the condition, a `COMPLETED`
  order was overwritten with `TIMEOUT`.
- `TestPublishCheckoutExpired_GoesWhereTheWaitroomReads`, against a renamed topic.
- `TestConfirmOrder_APaymentOnATimedOutOrderStillGetsItsTicket`. Without the `TIMEOUT`
  branch, the payment was refused as already processed.
- `TestExpireOrder_AFailedReleaseStillFreesTheChair`, against a release error returned.
- `TestCreate_ALostRaceStartsNoClock` (clock started before the saga's result) and
  `TestCreate_AFailedClockDoesNotFailThePurchase` (its error returned).
- `TestCreate_TheClockOutlivesTheCallersDeadline`. The clock was started on a
  cancelled context.

On k3s, 2026-09-30, revision 57 deployed `sha-0b0d097` to every app:
- `make -C deploy k3s-gate2` and `make -C deploy k3s-gate-sold-out` passed, so the
  purchase flow and the door are unchanged.
- `make -C deploy k3s-gate-checkout-expiry` passed on its first run. A's chair was freed
  556s after the order: the hold is 540s and the chair's own TTL 900s. A's order read
  `CANCELED` through the gateway and `TIMEOUT` in DynamoDB, and its session read
  `expired`. Its reservation read `CANCELLED`, which is `ExpireOrder`'s release, not
  `EXPIRED` from inventory's sweep. B's order stayed `COMPLETED`.
- Four clocks fired. The three on paid orders (gate 2's, the sold-out gate's, and B's)
  each logged `nothing to expire`; A's logged `Order timed out unpaid`. Every step
  except `ReleaseInventory` ran as a local activity, the path no unit test covers.
- `tb_order_checkouts_expired_total` read 1 on order-consumer. Inventory counted one
  `Release`, `OK`.
