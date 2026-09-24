# 0014 — New TS structure follows the June layout rule; no service is its template yet

**Date:** 2026-09-24
**Status:** accepted
**Arc:** ts-layout — [design](../design/ts-layout.md)
**Where it lives:** `docs/design/ts-layout.md`, root `CLAUDE.md` *Conventions that span services*

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Which TS layout is the target for new structure? | The 2026-06-16 rule, restored as a design: flat controller, one transport `dto/`, domain shapes in `<feature>.types.ts`, additions by archetype | Making `event-svc`'s current shape the rule | A rule no service follows yet, applied only as each service is converged |

## Context

The root `CLAUDE.md` called `event-svc` the converged reference, and in the same list
forbade the `controllers/grpc/dtos` + `dtos/` split `event-svc` has. The `add-service`
skill said not to copy it. `services/api-gateway/src/CLAUDE.md`, 818 lines that load
whenever gateway source is opened, prescribed `dtos/req` + `dtos/resp`.

The history explains the contradiction. A design of 2026-06-16 found that `event-svc`'s
two DTO layers are both legitimate: transport DTOs with validation and wire types, and
domain shapes. It gave a rule reconciling them, and chose `event-svc` as the first
service to converge. The plan to do that (`6ddb413`) was never executed. The design was
deleted with `docs/superpowers/` and the root file kept its conclusion, stated as done.

## Options

**Re-adopt the June rule.** It was already reasoned from the code, and it keeps what
the double layer protects. Costs a target no service meets.

**Make today's `event-svc` shape the rule.** Zero moves, and the docs match reality.
Costs standardising the nesting the June review found accidental.

**Drop a target.** "Match the service you're in." The four services stay divergent for
good, and each new module copies whichever one its author opened last.

## Decision

"Re-adopt the June rule" — the architect, 2026-09-24, choosing from the three options
above. The option as put to them included cutting the gateway's style guide down to
what is still true and moving no code now.

## Consequences

A new module follows the design, not its neighbours. An existing service converges
whole, in a change that is working on that service anyway: the payoff is
predictability, not capability. The first one converged becomes the worked example.
The gateway's `src/CLAUDE.md` keeps its conventions for clients, errors and auth, and
defers layout to the design.

## Outcome

`18116f3`. No code moved. The design, the root `CLAUDE.md`, the `add-service` and
`add-grpc-endpoint` skills and the gateway's `src/CLAUDE.md` now say the same thing, and
the system map routes to the design. Its check passed in CI on `f588c0f`. Whether the
rule holds is decided at the first convergence, which is also when a service becomes
its worked example.
