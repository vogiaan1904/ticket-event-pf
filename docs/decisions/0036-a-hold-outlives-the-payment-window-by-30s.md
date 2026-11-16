# 0036 — A hold outlives the payment window by 30s

**Date:** 2026-11-16
**Status:** proposed
**Arc:** late-payment — [plan](../plans/2026-11-14-a-late-payment-gets-its-ticket.md)
**Where it lives:** `services/order-svc/internal/workflows/shared.go` (`ReservationHoldGrace`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How long does a checkout's hold outlive its 6-minute payment window? | 30s, over ten times the measured webhook → `COMPLETED` p99 of 2.47s | 3 minutes, sized before anything was measured | A confirm stalled past 30s, while its ticket is resold, becomes `REFUND_REQUIRED` and pages |

## Context

`ReservationHoldGrace` covers a payment at the edge of the provider's 6-minute window
on its way through webhook → outbox → Kafka → `ConfirmOrder`. At 3 minutes, an
abandoned checkout keeps its ticket and its waitroom chair for 3 minutes after nobody
can pay any more. On k3s 2026-11-16, 372 purchases at the door of 2 a second measured
webhook → `COMPLETED` at p99 2.47s, max 2.52s
([plan, *Ticket 5 results*](../plans/2026-11-14-a-late-payment-gets-its-ticket.md#ticket-5-results)).

Since [0032](0032-a-payment-after-a-released-hold-gets-its-ticket-if-one-is-left.md),
a confirm that arrives after the hold was released still gets its ticket if one is
left. The grace decides only the contested case: a late confirm loses its ticket only
if another buyer took it in between.

## Options

**30s** (proposed). Ten times p99, and six relay safety polls (5s) for a missed NOTIFY.
An abandoned checkout frees its ticket and chair 2.5 minutes sooner.

**Keep 3 minutes.** Absorbs a relay or consumer stall of up to 3 minutes without a
refund, at the cost of 3 dead minutes per abandoned checkout, the minutes a sold-out
event's waiting buyers spend waiting on them.

**About 10s, p99 times four.** Frees most, but leaves one missed NOTIFY and its 5s
safety poll almost no room.

## Decision

The architect was shown the measurement and a proposal of 30s with its cost each way,
and answered "yes do what you propose", 2026-11-16.

Not put as a separate choice: the measurement covers the k3s door of 2 a second only.
A stall longer than 30s is 0017's page, not something any grace is sized for.

## Consequences

- An abandoned checkout ends at 6m30s instead of 9m: hold, order and chair together,
  since `CheckoutLifetime` sets all three.
- A confirm stalled past 30s on a sold-out ticket class refunds and pages through
  `OrdersNeedingRefund`, where 3 minutes would have absorbed it.
- Inventory's expiry sweep runs every 60s, longer than the grace, so a hold is swept
  between 6m30s and 7m30s; `ExpireOrder`'s release at 6m30s usually comes first.
- Holds while the confirm leg stays near its measured lag. A door well above 2 a
  second needs it measured again.
