# 0005 — Buyers who wait for the doors are ordered by lot; latecomers by arrival

**Date:** 2026-09-23
**Status:** accepted
**Arc:** waitroom-stampede — [plan](../plans/2026-09-23-waitroom-join-stampede.md)
**Where it lives:** `services/waitroom-svc/internal/models/session.go:91`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Who goes first? | A random draw in the second before the sale for pre-open joiners, drawn once and stored; arrival order after | Arrival order (`QueuedAt.Unix()`) | Joining early buys a place in the draw, not at the front |

## Context

The queue score was `QueuedAt.Unix()`, so an on-sale was won by whoever completed
a round trip first — a race between network paths and scripts, which a bot always
wins. Ties within a second already fell to session UUID order: random, but
undocumented and unintended.

## Options

**Arrival order.** Simple; rewards the fastest client.

**A draw for everyone who joined before the sale opens.** Scores in
`[saleStart-1, saleStart)`, so every pre-open buyer sorts ahead of every latecomer
and among themselves by lot.

## Decision

Reconstructed after the fact from the stampede plan (phase 4) and commit
`a09b859`; the decider's words were not recorded.

## Consequences

The band is closed: no draw reaches the first post-open score. The score is stored
on the session, because re-deriving it would move people already in line. A draw
only orders buyers who are waiting at the same time, so it depends on nothing
being admitted before the sale opens — which did not hold until
[0006](0006-admission-waits-on-the-queues-own-scores.md).

## Outcome

`a09b859`. The draw was inert for the first `MaxConcurrent` buyers until `323bcc7`
made admission wait for the sale; `6a2f1db` pins its band and order in a real
sorted set.
