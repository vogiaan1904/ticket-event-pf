# 0026 — The order contract tells a buyer what happened, and only to that buyer

**Date:** 2026-10-01
**Status:** accepted
**Arc:** order-contract — [plan](../plans/2026-10-01-the-order-contract.md)
**Where it lives:** `proto/order.proto`, `services/order-svc/internal/order/delivery/grpc/presenter.go`, `services/order-svc/internal/order/service/order.go` (`Cancel`, `GetByID`), `services/api-gateway/src/modules/orders/`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What does the order contract tell a buyer, and who may read or cancel an order? | Every stored status gets its own wire value; order-svc checks the owner on read and cancel, with `user_id` in the contract; a cancel is a conditional `PENDING` → `CANCELLED` write; the gateway's routes follow the contract | Keeping `TIMEOUT` as `CANCELED` and refunds as `UNSPECIFIED`; an owner check in the gateway only; removing cancel | A contract change across order-svc and the gateway; until both have rolled, an old gateway's reads are refused for want of `user_id` |

## Context

The gateway's order module was written against an older contract, with orders keyed
by `id` and paged by number. order-svc keys them by `code` and pages by cursor. So:
- `GET /orders/:id` and `DELETE /orders/:id` send `id`, which order-svc does not know,
  and are refused every time.
- `GET /orders` sends a page number; order-svc refuses any request without a cursor,
  so the first page cannot be read.
- `GET /orders/code/:code` works, for anyone signed in: nothing checks that the order
  is the caller's, and it carries the buyer's name and email.

What the contract can say is short of what order-svc stores. It has no value for an
order that timed out (0024 sends it as `CANCELED`), one that is owed a refund, or one
that was refunded; the last two arrive as `UNSPECIFIED`. A buyer who was charged and
is owed money cannot tell.

`Cancel` reads the order, checks `PENDING`, releases its tickets, then writes
`CANCELLED` unconditionally. A payment that confirms between the read and the write
is overwritten, and its tickets are already back on sale. Only the gateway's broken
route kept this unreachable.

## Options

**Statuses on the wire.**
- *One wire value per stored status* (chosen): `EXPIRED` for `TIMEOUT`,
  `REFUND_REQUIRED`, `REFUNDED`. Additive enum values; a gateway that predates them
  reads them as unknown.
- *Keep the mapping*: no contract change, and a charged buyer still reads
  `UNSPECIFIED`.

**Who may read or cancel an order.**
- *order-svc checks the owner* (chosen), with `user_id` on `GetOrderRequest` and
  `CancelOrderRequest`, as 0008 did for waitroom sessions. A stranger gets 404, so an
  order's existence is not confirmed to them.
- *The gateway checks*: it would have to fetch the order to learn its owner before
  every cancel, and any other caller of order-svc would have no check at all.

**Cancel.**
- *A conditional write* (chosen), the shape `ExpireIfPending` already uses: flip
  `PENDING` to `CANCELLED` first, and release the tickets only if the flip happened.
- *Remove cancel*: the route has never worked, so nobody depends on it; but a buyer
  who changes their mind would hold tickets until the order times out.

**Routes.** `GET /orders/code/:code` stays as the scripts and load test call it;
`DELETE /orders/code/:code` replaces `DELETE /orders/:id`; `GET /orders/:id` is
removed. The list takes `cursor` and `limit` and answers `nextCursor` and `hasNext`.

## Decision

Delegated: "yes, just push commit, then resolve the order contract first, then
continue with the sell out, plan and do it yourselft automatically" (2026-10-01).
The options above were weighed by the agent; none was put to the architect as a
separate choice.

## Consequences

- A buyer sees `EXPIRED` for a checkout that timed out, and `REFUND_REQUIRED` or
  `REFUNDED` when money is owed or returned.
- Reading or cancelling someone else's order answers 404.
- A cancel that loses to a payment answers 409 and changes nothing.
- The gateway's order routes and their tests describe the contract that runs.

## Outcome

Built in `71f737f` (statuses), `f82ae31` (the owner), `c1fdcc1` (cancel) and `b1d8379`
(the gateway); the acceptance gate in `6077629`. Its first run on k3s read the order
back and found three fields that had never been stored, since nothing had read an order
back before: the phone, the payment method, and each item's quantity and price, which
read 0. `f4a7bb3` carries them through.

Each test below failed before its code existed, and fails again when the code it guards
is broken:
- `TestEveryStoredStatusHasItsOwnWireValue`. `REFUND_REQUIRED` and `REFUNDED` read
  `UNSPECIFIED`, and `TIMEOUT` shared `CANCELED`.
- `TestGetByID_AStrangerIsToldThereIsNoSuchOrder` and
  `TestCancel_AStrangerIsToldThereIsNoSuchOrder`, without the owner check.
- `TestCancel_APaymentThatLandedFirstWins`. The old `Cancel` overwrote a payment that
  confirmed after its read, and released its tickets. Dropping the write's condition
  fails it, and both `LeavesAPaidOrderAlone` repository tests.
- `TestGetManyOrders_TheFirstPageNeedsNoCursor`, against the old validation.
- `TestCreateOrder_StoresWhatTheBuyerAskedFor` and
  `TestCreateOrderInput_KeepsEveryFieldTheBuyerSent`, against the dropped fields.
- In the gateway, the unknown-status and caller tests, against a mapper that throws and
  a read that sends no `user_id`.

On k3s, 2026-10-01, revisions 60 and 61 (`sha-f4a7bb3`, then `sha-895d7b1`):
- `make -C deploy k3s-gate-orders` passed. The owner read the order with its 2-ticket
  line, phone and payment method, and found it on the first page of the list. A
  stranger got 404 for both read and cancel, and the order stayed `PENDING`. The owner's
  cancel answered 200; the order read `CANCELED`, its reservation `CANCELLED`, and a
  second cancel 409.
- gate 2 and the sold-out gate passed on both revisions, and the room gate on 61.
- `make -C deploy k3s-gate-checkout-expiry` passed on 61: the unpaid order read
  `EXPIRED` on the wire 555s after it was placed, with `TIMEOUT` stored.
