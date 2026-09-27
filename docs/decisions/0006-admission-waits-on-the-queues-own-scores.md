# 0006 — Admission waits on the queue's own scores, not on event-svc

**Date:** 2026-09-23
**Status:** accepted
**Arc:** waitroom-admission — [plan](../plans/2026-09-23-waitroom-admission-correctness.md)
**Where it lives:** `services/waitroom-svc/internal/service/queue_service.go:156`, `services/waitroom-svc/pkg/redis/client.go:130`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| When may admission start? | Nothing is admitted until its score's second has passed | The processor asks event-svc for the sale time each tick | Up to 1s extra wait after opening; a sale time moved after people joined is not honoured for them |

## Context

The processor admitted whenever a slot was free and never read the sale start.
The first `MaxConcurrent` pre-open joiners were admitted in arrival order before
the doors opened, on tokens that `Reserve` refuses until the ticket class opens,
and the draw of [0005](0005-waiting-buyers-are-ordered-by-lot.md) ordered only the
overflow.

## Options

**The queue's own scores.** `PeekQueue` reads only scores below the current whole
second. Every pre-open draw sits below the sale start, so none is due until then.
No new dependency.

**Ask the event gate each tick.** Honours a moved sale start, but puts event-svc in
the admission loop: with the cache cold and event-svc down, the processor must hold
everyone (a stalled on-sale) or admit (this bug again, exactly when it matters).

## Decision

Delegated: "And for D1, and D2, choose what you recommened" — the architect, 2026-09-23.

## Consequences

When admission may start is data already in Redis, not a call. A post-open joiner
waits up to one extra second. If a sale start moves after people join, their
scores keep the old band.

## Outcome

`323bcc7`, with real-Redis tests that fail on the old code and a mutation check
against holding too long. On k3s the gate and 310 load purchases were admitted
under it.
