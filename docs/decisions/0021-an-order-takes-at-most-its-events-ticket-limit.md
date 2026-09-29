# 0021 — An order takes at most its event's ticket limit

**Date:** 2026-09-29
**Status:** proposed
**Arc:** admission-sizing — [design](../design/admission-sizing.md#tickets-per-order), [plan](../plans/2026-09-29-tickets-per-order.md)
**Where it lives:** `proto/event.proto` (`EventConfig.max_tickets_per_order`), `services/order-svc/internal/order/service/order.go` (`Create`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How many tickets may one order take? | A limit per event, default 4, checked in order-svc before anything is held | No limit; one limit for every event; a limit per ticket class | A field through five layers of event-svc and the gateway, and 0 on the wire has to mean "not set" |

## Context

Nothing caps an order today. The gateway requires only `quantity >= 1`, the list of
items has no length limit, and neither order-svc nor inventory checks a total. One
buyer in one waitroom chair can reserve every ticket left, so no number of chairs
bounds the tickets held.

## Options

**A limit per event.** The organizer sets it with the rest of the event's config;
order-svc already fetches that config on every create. It costs a field through
event-svc and the gateway, a migration, and regenerated stubs.

**One limit for every event.** A constant in order-svc and nothing else. But a family
show that wants 6 and a hot concert that wants 2 cannot both be served.

**A limit per ticket class.** VIP at 2 and general at 6. The finest control, and a
change to inventory's model and to how an order's items are checked against it.

**No limit.** Leaves hoarding open and the chair arithmetic unbounded.

## Decision

The architect asked: "first, for the how many tickets one buyer can take per order,
whats currently behavior ? what if we change to a fixed limited number or per event
config ?" (2026-09-29). The answer compared the three limits and recommended one per
event. The architect then asked for the plan: "now think deeply and write plan for
this, then make sure you can compact this session and still able to run this plan"
(2026-09-29).

These came inside that plan and were never put as their own choice:
- the default of 4;
- the range of 1–10;
- 0 meaning "not set";
- `INVALID_ARGUMENT`;
- counting across all of an order's items.

## Consequences

- Tickets held are bounded by chairs × the limit, which the room-size rule (step 4
  of the design's *What comes next*) needs.
- Every existing event gets the default of 4 from the migration.
- A client that omits the field on update keeps the stored limit; a buyer asking for
  more gets a 400 that names `ORD020`.
- An order-svc reading 0 checks nothing, so a rolling deploy in either order refuses
  no order that it would not have refused before.
