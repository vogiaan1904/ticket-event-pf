# Reservation hold window

The inventory hold an order takes must outlive the payment window, or a buyer
who pays at the edge of it ends up paid with no seat.

```
hold expiry = PaymentTimeout (6m) + ReservationHoldGrace (3m)
```

`internal/workflows/shared.go`.

## Why the grace exists

A payment completing at the very edge of `PaymentTimeout` still has to travel
`webhook -> outbox -> Kafka -> ConfirmOrder`, and inventory's expiry worker
sweeps every 60s. Without the slack the worker wins that race.

## What it does not cover

It is sized for the happy path, **not** a retry storm. A `ConfirmOrder` that
exhausts `getConfirmOrderActivityOptions`' policy burns ~8 minutes of backoff
alone, well past the grace.

That tail is covered on the other side: inventory's `Confirm` re-acquires a hold
the worker already swept, as long as the stock has not been resold. The grace
removes the common race; the re-acquire is the backstop.
