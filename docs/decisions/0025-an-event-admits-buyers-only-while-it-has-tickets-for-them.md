# 0025 — An event admits buyers only while it has tickets for them

**Date:** 2026-10-01
**Status:** accepted
**Arc:** admission-sizing — [design](../design/admission-sizing.md#room-size-per-event), [plan](../plans/2026-10-01-room-size-per-event.md)
**Where it lives:** `services/waitroom-svc/internal/service/queue_processor.go` (`admitCount`), `services/waitroom-svc/internal/service/stock_gate.go` (`Stock`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How many buyers may hold inventory at once, and can it vary per event? | As many as the event has tickets available: a tick admits only while `available` exceeds the buyers inside, and the global 100 is deleted | Inventory counting the orders that hold, so a buyer holding tickets is not counted twice; an organizer field on `EventConfig`; keeping 100, globally or per target | The last tickets of a sell-out can wait up to one payment time for a chair to free; no lever caps chairs on its own |

## Context

Room size is how many buyers may be inside an event at once: admitted and holding a
checkout pass. It is `QUEUE_DEFAULT_MAX_CONCURRENT`, 100, for every event.

- **For real buyers it is the limit, not the door.** A full room admits only as fast
  as buyers leave, 100 ÷ stay. At a 3-minute stay that is 0.56 a second, against
  k3s's measured door of 2 and EKS's default of 10. Load tests stay 2–4s, so the room
  never fills there, and no test shows it. 0019 measured the door under exactly that
  condition.
- **It knows nothing about tickets.** A 3-ticket event admits up to 100. A buyer whose
  checkout `Reserve` refuses keeps their chair for the token's 15 minutes, since
  nothing tells the waitroom.

Door speed already bounds the machine (0019), and a buyer who is paying costs it
nothing. Matching buyers to tickets is all that is left for room size. The tick
already asks inventory for the event's counts each second
(`services/waitroom-svc/internal/service/stock_gate.go`), and keeps only the door
they imply.

## Options

**What limits the buyers inside an event.**
- *Its tickets available now* (recommended): a tick admits only while `available`
  exceeds the buyers inside. A waitroom change only, with no new field. A buyer who
  already holds tickets is counted twice, so the last tickets can wait up to one
  payment time for a chair to free.
- *`available` minus the buyers inside who hold nothing yet*, with inventory counting
  the orders that hold tickets, an additive field on `GetEventStock`. The tail sells at
  full speed. It costs a contract field, a count per answer, and two stores read at
  slightly different moments. An inventory without the field answers 0, which reads
  as the first option.
- *An organizer field on `EventConfig`*, as 0021 did. Explicit, but static: it cannot
  follow tickets left, and it asks organizers for a number that depends on the
  target's door speed.
- Not offered: *`total − sold` as the event's size.* It counts held tickets rather
  than the buyers holding them, so with 2-ticket orders it admits about twice the
  buyers the tickets can serve.

**What happens to the global 100.**
- *Delete it* (recommended). Each tick admits at most the door's batch and a chair
  lives at most the token's 15 minutes, so chairs never exceed door speed × 900s:
  1800 on k3s, 9000 on EKS. A cap below that only holds admission under the measured
  door. No lever is left to cap chairs on their own, if a payment provider ever needs
  one.
- *Keep it per target, sized door × chair lifetime.* Explicit, but it goes stale each
  time the door is measured again, and then caps throughput again.
- *Keep 100.* Real buyers keep flowing at 100 ÷ stay.

## Decision

The architect was given the two calls, each with a recommended option, its
alternatives and their costs, as above. They asked for the findings in plainer terms
and checked their own model of a tick. They then answered: "1 A, 2 A" (2026-10-01).
That means:
1. a tick admits only while the event has more tickets available than buyers
   inside;
2. `QUEUE_DEFAULT_MAX_CONCURRENT` is deleted.

## Consequences

- Room size varies per event with no configuration: it follows each event's tickets
  available, tick by tick.
- With nothing to judge by, inventory unanswered or no class that can still sell, the
  door speed alone paces admission.
- `paused` keeps meaning "no ticket available". A waiter held back only by the room
  sees their position stop moving, not `paused`.
- The 100 also held real buyers under EKS's unmeasured door of 10 a second. Without
  it, that door is EKS's only limit. EKS has no real buyers, and its load tests never
  filled the room, so nothing measured changes.
- Gate 4a's admission check becomes "peak buyers inside ≤ the event's tickets".

## Outcome

Built in `758466d` and `b6e08bd`; the acceptance run in `60337be`. Its first run on k3s
expected a waiter's `paused` to read `False`, which the gateway leaves out of the body
when false; `8e6fae5` reads it absent as not paused.

Each test below failed before its code existed, and fails again when the code it guards
is broken:
- `TestStockGateCountsNothingWhenNoClassCanSell`, with `Counted` always true: an event
  with no class that can still sell got a room of 0.
- `TestTheRoomAdmitsOnlyAsManyAsTicketsLeftOver` and
  `TestNoTicketBeyondTheBuyersInsideAdmitsNobody`, with `- inside` dropped: the tick
  admitted up to the tickets available whatever the buyers inside.
- `TestWithNothingToJudgeByOnlyTheDoorPaces`, with the uncounted branch removed: a down
  inventory shut the door.
- `TestAdmitCount`, under both breaks.

On k3s, 2026-10-01, revision 58 deployed `sha-60337be` to every app. `waitroom-config`
has no `QUEUE_DEFAULT_MAX_CONCURRENT`, and the waitroom logged
`Starting queue processor - interval: 1s, batch_size: 2`:
- `make -C deploy k3s-gate2` and `make -C deploy k3s-gate-sold-out` passed, so admission
  and the door are unchanged where tickets are plenty and where they run out.
- `make -C deploy k3s-gate-room` passed. Three tickets, five buyers who never order: 3
  inside and 2 waiting, still so 5s later at a door of 2 a second. A waiter read
  `QUEUED`, not paused, at position 1. Two tickets added: 5 inside, 0 waiting. The
  first run held the same 3 and 2 before failing on the `paused` check.
- Not observed: the gate going red on the code before this change, which would admit
  all five. That build was not deployed.
