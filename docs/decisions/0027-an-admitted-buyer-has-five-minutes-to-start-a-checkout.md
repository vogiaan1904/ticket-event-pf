# 0027 — An admitted buyer has five minutes to start a checkout

**Date:** 2026-10-01
**Status:** proposed
**Arc:** admission-sizing — [design](../design/admission-sizing.md#the-end-of-a-sell-out), [plan](../plans/2026-10-01-the-end-of-a-sell-out.md)
**Where it lives:** `deploy/helm/ticketbottle/values.yaml` (`waitroom.checkoutWindow`), read as `JWT_EXPIRY` by `services/waitroom-svc/config/config.go`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How long may an admitted buyer hold a place before starting a checkout? | 5 minutes: the checkout token and its chair both last 5 minutes, down from 15 | A `checkout.started` event that frees a buyer's chair once their order holds tickets; freeing a refused buyer's chair at once; 3 or 10 minutes; keeping 15 | A buyer slower than 5 minutes joins again, behind whoever waits; a retried `POST /orders` after 5 minutes cannot recover a lost payment link |

## Context

Since 0025 a tick admits only while the event has more tickets available than buyers
inside, so every chair stands for a ticket. Three kinds of chair stand for one without
taking it, each until the token's 15 minutes end:
- a buyer admitted who never orders;
- a buyer whose `Reserve` was refused, since order-svc tells the waitroom nothing;
- a buyer whose order holds tickets: `available` already leaves those tickets out, so
  the buyer counts twice until the order settles, up to the hold's 9 minutes.

At the end of a sell-out each holds back one buyer. Three such chairs on the last three
tickets stop the line for up to 15 minutes while the tickets go unsold.

The token is checked once, when the order is created. After that the hold (9 minutes)
and the order's own clock (0024) govern the checkout, so the token's lifetime is only
the time a buyer has to start one.

## Options

**What bounds a chair that holds a place without taking a ticket.**
- *A short window to start a checkout* (chosen): the chair lives as long as the token,
  so one value bounds all three kinds. A chart value; no code changes.
- *A `checkout.started` event*: order-svc publishes when an order takes its hold, and
  the waitroom frees that buyer's chair. It removes the double count rather than
  bounding it, but adds a topic, a producer, a consumer and their dead-letter path,
  and leaves the other two kinds at 15 minutes.
- *Free a refused buyer's chair at once*: order-svc publishes `checkout.failed` when
  `Reserve` refuses. The buyer loses the chance to retry with fewer tickets.

**How long.**
- *5 minutes* (chosen): time to pick a class and a quantity, a third of the old bound.
- *3 minutes*: a faster tail, and a hurried buyer.
- *10 minutes*: gentler, and two thirds of the old stall.

## Decision

Delegated: "yes, just push commit, then resolve the order contract first, then
continue with the sell out, plan and do it yourselft automatically" (2026-10-01).
The options above were weighed by the agent; none was put to the architect as a
separate choice.

## Consequences

- At the end of a sell-out a chair that takes no ticket holds a place for at most 5
  minutes.
- Chairs never exceed door speed × 300s: 600 on k3s, 3000 on EKS.
- A checkout started in time is unchanged. Its chair usually ends at 5 minutes, before
  the hold does, which removes the double count from then on.
- `checkout.expired` no longer frees chairs in practice: the window ends first. It
  still ends the session and invalidates the token.
