# 0032 — A payment after a released hold gets its ticket if one is left

**Date:** 2026-11-14
**Status:** proposed
**Arc:** late-payment — [plan](../plans/2026-11-14-a-late-payment-gets-its-ticket.md)
**Where it lives:** `proto/inventory.proto` (`ReleaseReason`), `services/inventory-svc/internal/services/reservation.go` (`cancelReservationTx`), `services/order-svc/internal/workflows/confirm_order.go` (`confirmable`), `services/order-svc/internal/workflows/expire_order.go`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What does a payment get when it lands after its checkout ended, by timeout or by the buyer's cancel? | Its ticket if one is left, else a refund: `Release` carries a reason, an ended checkout is written `EXPIRED`, which `Confirm` re-acquires, and `ConfirmOrder` treats a `CANCELLED` order like a `TIMEOUT` one | `Confirm` re-acquiring `CANCELLED` holds, guarded only by order-svc; a refund for every payment after a cancel | A proto field regenerated in every consumer; `EXPIRED` now also means "released while its link may still be paid"; a buyer's cancel no longer refuses a later payment |

## Context

[0024](0024-an-unpaid-order-times-out-when-its-hold-expires.md) chose: a payment on a
`TIMEOUT` order is confirmed if inventory can re-acquire the ticket. That ran only
when inventory's expiry worker beat `ExpireOrder` to the hold. In the normal case
`ExpireOrder`'s `Release` wrote the hold `CANCELLED`, and `Confirm` re-acquires only
`EXPIRED` holds. Every late payment was refunded and paged, even with stock free.
The order-side test mocked `ConfirmInventory`, so nothing caught it.

[0026](0026-the-order-contract-tells-the-buyer-what-happened.md)'s owner cancel releases
the hold, but the ZaloPay link stays payable until its own expiry (6 minutes from
creation). payment-svc routes only ZaloPay, and `CancelPayment` is a stub. A payment
after a cancel was refunded by `ConfirmOrder`, and paged.

## Options

**How inventory tells a re-acquirable release from a final one.**
- *`ReleaseRequest` carries a reason* (recommended). Inventory keeps its own rule:
  `EXPIRED` is re-acquirable, `CANCELLED` is final. Costs a proto change regenerated in
  every consumer. Proto3's zero default keeps an old caller on today's behaviour.
- *`Confirm` re-acquires `CANCELLED` too.* No contract change, but inventory can no
  longer refuse a hold undone on purpose. Correctness then rests on order-svc's status
  check alone.

**What a payment after a buyer's cancel gets.**
- *Re-acquire like a timeout* (recommended). Reuses the same path, and turns a page into a sale.
- *Keep refunding.* Every cancel-then-pay pages, and auto-cancelling an abandoned pay
  page would multiply that.
- *Cancel the provider intent.* Needs a ZaloPay cancel API, which no gateway here has.

## Decision

The architect was given both calls, each with a recommended option, its alternatives
and their costs as above. They answered (2026-11-14):
1. "Release carries a reason (Recommended)";
2. "Re-acquire like a timeout (Recommended)".

That means:
1. `RELEASE_REASON_CHECKOUT_ENDED` writes `EXPIRED`. Unspecified, sent by saga
   compensation and a failed payment, writes `CANCELLED`.
2. `ExpireOrder` and `Cancel` release as `CHECKOUT_ENDED`. `ConfirmOrder` lets a
   `CANCELLED` order through to `ConfirmInventory`; `PAYMENT_FAILED` is still refunded.

Not put as a separate choice: reusing `EXPIRED` for a buyer's cancel rather than adding
a status. That came inside the approved plan.

## Consequences

- `REFUND_REQUIRED` is left for a late payment whose seat was really resold. That is
  the case `OrdersNeedingRefund` should page for.
- A buyer's cancel is a release, not a refusal: the seat goes back to the pool, but a
  payment that still lands on the open link wins it back if nobody took it.
- Both workflow changes sit behind `workflow.GetVersion`
  (`expire-releases-ended-checkout`, `confirm-cancelled-order`). An instance already
  running at deploy finishes on the old path.
- Holds to keep: a failed payment and saga compensation must never send
  `CHECKOUT_ENDED`, or a refused payment could take a seat.
