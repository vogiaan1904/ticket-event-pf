# 0004 — Event rules are cached per event, and concurrent misses share one fetch

**Date:** 2026-09-23
**Status:** accepted
**Arc:** waitroom-stampede — [plan](../plans/2026-09-23-waitroom-join-stampede.md)
**Where it lives:** `services/waitroom-svc/internal/service/event_gate.go:53`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What does a join ask event-svc? | A per-event cache (30s); concurrent misses share one call; verdicts cached, dependency failures not | Two event-svc calls per joiner | Event rules can be up to 30s stale |

## Context

`JoinQueue` made two synchronous event-svc calls per joiner for answers that do
not change during an on-sale. 500k joiners meant a million calls into event-svc
and the Postgres it shares with Temporal at a stock `max_connections=100`.

## Options

**Call per joiner.** Always fresh; the load scales with the crowd.

**A TTL cache alone.** Cold at the instant an on-sale opens, so every joiner misses
together — the stampede it was meant to prevent.

**A TTL cache with `singleflight`.** Concurrent misses collapse into one call, run
on a detached context so one joiner hanging up does not fail the rest.

## Decision

Reconstructed after the fact from the stampede plan (phase 3) and commit
`2049d1a`; the decider's words were not recorded.

## Consequences

event-svc sees about one call per event per TTL. "No such event" is cached as a
success is; a dependency failure is not, or a queue would stay shut for the rest
of the TTL after event-svc recovered. A rule change reaches joiners within 30s.

## Outcome

`2049d1a`, with unit tests for the collapse and the no-cache-on-failure rule. On
k3s every join in the gate and the load run passed through it to a real event-svc.
