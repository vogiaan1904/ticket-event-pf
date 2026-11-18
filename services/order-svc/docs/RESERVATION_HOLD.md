# Reservation hold window

The inventory hold an order takes must outlive the payment window, or a buyer
who pays at the edge of it ends up paid with no seat.

```
hold expiry = CheckoutLifetime = PaymentTimeout (6m) + ReservationHoldGrace (30s)
```

`internal/workflows/shared.go`. `CheckoutLifetime` is also the delay before
`ExpireOrder` times out an order nobody paid for, so the order, the hold and the
buyer's chair end together.

## Why the grace exists

A payment completing at the very edge of `PaymentTimeout` still has to travel
`webhook -> outbox -> Kafka -> ConfirmOrder`. 30s is ten times that leg's p99,
measured on k3s at 2.47s
([0036](../../../docs/decisions/0036-a-hold-outlives-the-payment-window-by-30s.md)).
Inventory's expiry worker frees only holds already past their expiry, so it never
takes a hold inside its grace.

## What it does not cover

It is sized for the happy path, **not** a retry storm. A `ConfirmOrder` that
exhausts `getConfirmOrderActivityOptions`' policy burns ~8 minutes of backoff
alone, well past the grace.

That tail is covered on the other side: inventory's `Confirm` re-acquires a hold
the worker already swept, as long as the stock has not been resold. The grace
removes the common race; the re-acquire is the backstop.

The same backstop covers a payment that lands after `ExpireOrder` timed the order out,
or after the buyer cancelled it. Both release the hold as `CHECKOUT_ENDED`, which
inventory writes `EXPIRED`, so `Confirm` can re-acquire it. `ConfirmOrder` confirms a
`TIMEOUT` or `CANCELLED` order like a `PENDING` one, and marks it `REFUND_REQUIRED`
only when inventory cannot re-acquire the ticket
([0032](../../../docs/decisions/0032-a-payment-after-a-released-hold-gets-its-ticket-if-one-is-left.md)).
