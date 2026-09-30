# 0023 — The waitroom stops admitting when no ticket is left

**Date:** 2026-09-30
**Status:** proposed
**Arc:** admission-sizing — [design](../design/admission-sizing.md#when-tickets-run-out), [plan](../plans/2026-09-30-waitroom-knows-when-tickets-run-out.md)
**Where it lives:** `services/waitroom-svc/internal/service/stock_gate.go`, `proto/inventory.proto` (`GetEventStock`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What does the waitroom do when an event's tickets run out? | Asks inventory each tick; pauses while nothing is available; ends the line once sold out; fails open | Admit regardless; inventory sends a message; keep waiters on a sold-out line; fail closed | Inventory joins the join and status paths, cached for half a tick; a waiter loses their place if tickets come back |

## Context

The door admits at its speed whatever inventory has left. Once every ticket is held
or sold, each buyer it admits costs the box a checkout that inventory refuses, and
holds a chair for the token's 15 minutes. Everyone behind waits for nothing.

Inventory could not answer the question. `FindManyTicketClass` returns no reserved or
sold counts, and `GetAvailability` covers one class, not an event.

## Options

**How the waitroom learns.**
- *Asks inventory each tick* (chosen). Correct after any failure: the next answer
  replaces a wrong one.
- *Inventory sends a message.* Inventory has no outbox, so a lost "tickets came back"
  message would leave a line paused forever.

**Where the answer is kept.**
- *One answer per event in the waitroom's memory, shared by the tick, joins and status
  polls* (chosen). It is the event gate's pattern: concurrent misses collapse, and a
  status poll costs no extra round trip.
- *The tick writes the door to Redis, and joins and polls read it.* Inventory has one
  caller. But a join is refused only while the key is fresh; an empty line stops the
  tick asking, so the key ages out; and every status poll adds a Redis read.

**What a sold-out line does to the people in it.**
- *Ends their sessions* (chosen). The line empties, and the tick stops asking.
- *Keeps them.* The line would resume in order if tickets came back. But entries leave
  the line only when the tick tries to admit them, so a closed line keeps its dead
  entries. Without a sweeper, the tick would ask inventory about that event every
  second, forever.

**How a waiter hears it.**
- *`paused` on the status response, and 409 `WTR012` for sold out* (chosen). An ended
  session already answers a status poll with 409, and an old gateway ignores a new
  field.
- *New `SessionStatus` values.* The gateway throws on a value it does not know
  (`services/api-gateway/src/modules/waitroom/mappers/session-status.mapper.ts`), so
  every status poll would return 500 until the gateway rolled.

**When inventory does not answer.**
- *Open* (chosen). The waitroom admits exactly as before, and inventory's guarded
  `UPDATE` still stops an oversell.
- *Closed.* An inventory blip, or a broken path from the waitroom alone, would stop a
  sale that could still sell.

**What inventory returns.**
- *Counts, and the waitroom decides* (chosen). Inventory keeps the rule for what is on
  sale, and the waitroom keeps the rule for its door.
- *A verdict.* It would put the door's vocabulary inside inventory.

## Decision

Asking inventory each tick, rather than waiting for a message, came with the design's
*What comes next*, agreed on 2026-09-29.

The architect was asked what happens to the people still in line when the last ticket
sells. They were offered two answers, end their sessions (recommended) or keep them,
and answered "oke do it" (2026-09-30).

Everything else was delegated: "deeply think as an expert SA about this problem again
and choose the best match approach and auto do it for me" (2026-09-30). That
delegation came while how a waiter hears it was still an open question, with the
`paused` field recommended.

## Consequences

- Inventory answers on the join and status paths. A cold answer makes a join wait on
  inventory for up to 1s, and then it fails open.
- A waiter on a line that sells out loses their place. If an organizer later adds
  tickets, the door reopens, and they join again.
- A sale that is over, with every class past its end and tickets unsold, still admits.
  Its buyers get "sale closed" at checkout, as today.
- Chairs can still outnumber the tickets left. That is step 4 of the design, room size
  per event.
