# 0034 — event-svc converges to the TS layout first, as a change of its own

**Date:** 2026-11-14
**Status:** accepted
**Arc:** ts-layout — [plan](../plans/2026-11-14-event-svc-converges-to-the-layout.md), [design](../design/ts-layout.md)
**Where it lives:** `services/event-svc/src/modules/events/`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Which TS service converges first, and becomes the worked example? | event-svc, in a change that does nothing else | user-svc, the smallest; waiting until a service is changed anyway | A 71-file move with no capability, against the design's own "only when being worked on anyway" |

## Context

[0014](0014-ts-structure-follows-the-june-layout-rule.md) re-adopted the layout rule and
left every service off it until one converges. The design says to converge a service
only alongside other work on it. [0031](0031-the-next-work-is-ranked-by-what-a-buyer-can-hit.md)
ranked a convergence as standalone item 3, so the two disagree, and no service has a
change pending that would carry one.

## Options

**event-svc** (recommended). Domain-service archetype, and the case that shows the
transport `dto/` against `<feature>.types.ts` split; payment-svc can follow it
directly. The June plan's pick. Costs the larger move.

**user-svc.** Small-service archetype, 30 files, cheapest. Has no DTO split to show,
so it teaches the two domain services little.

**payment-svc.** The only service with two transports; the least typical first example.

## Decision

The architect was asked which service converges first, with the note that doing it
standalone overrides the design's rule, and answered "event-svc (Recommended)",
2026-11-14.

## Consequences

- The design's "only when being worked on anyway" is waived for this one change; it
  still holds for the others.
- event-svc becomes the worked example, so its shape is what payment-svc copies.

## Outcome

Moved in three commits, each green on build and tests: `93d4ffc` (flat controller,
`dto/`, `mappers/`), `a6aa617` (domain shapes to interfaces in `events.types.ts`),
`9f119dd` (the design's six aliases); docs in `c440c7d`. The suite stayed at 6 suites
and 37 tests with the repository suite on a real Postgres. The built service booted and
answered `FindMany`, `FindOne` and `GetConfig` over gRPC. On k3s 2026-11-18,
`sha-6ff19a0`, `k3s-gate2` passed. The move found one defect it did not fix: `FindMany`
with no filter answers `INTERNAL`
([plan, *Results*](../plans/2026-11-14-event-svc-converges-to-the-layout.md#results)).
