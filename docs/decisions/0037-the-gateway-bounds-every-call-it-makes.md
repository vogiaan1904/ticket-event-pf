# 0037 — The gateway bounds every call it makes

**Date:** 2026-11-18
**Status:** proposed
**Arc:** gateway-recovery — [plan](../plans/2026-11-16-gateway-calls-carry-a-deadline.md)
**Where it lives:** `services/api-gateway/src/shared/grpc/client-options.ts`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What bounds the gateway's calls to a backend that answers slowly? | A deadline on every call from one gateway interceptor: 5s, and 65s for `CreateOrder` | Also cancelling the call when the HTTP client leaves; TS backends skipping a query once its call's deadline has passed | A read slowed past 5s answers 504 instead of eventually succeeding; a TS backend still drains its own backlog |

## Context

The gateway set no deadline on any gRPC call. On k3s 2026-11-16, after a forced stall,
event-service held up to 476 calls in flight against the gateway's 200, and the gateway
recovered 16s after the lift where the channel bound
([0035](0035-the-gateway-bounds-a-backend-reconnect.md)) allows about 6s.

Measured locally 2026-11-18 ([plan, *Task 1 results*](../plans/2026-11-16-gateway-calls-carry-a-deadline.md#task-1-results)):
a TS handler runs to the end after its call's deadline, and a backlog queues inside
Prisma's pool, not ahead of the handler. All handlers start at once; their queries run
in turn, the last one 7s after its deadline.

## Options

**A. A deadline on every call, by method** (chosen). One grpc-js interceptor beside
`BoundedChannel`, in the same `channelOptions`. The gateway answers 504 at the bound
instead of hanging. A TS backend's work is not stopped.

**A + B: also cancel the call when the HTTP client disconnects.** Frees a Go handler at
once; a TS handler ignores the cancel, as measured. Costs request-close plumbing at the
HTTP layer.

**A + C': TS repositories skip their query once the call's deadline has passed.** Sheds
the queue where it was measured to form. Costs carrying the deadline into every
repository in three services. The plan's C, a check when the handler starts, sheds
nothing, since every handler starts in time.

## Decision

The architect was shown the measurement and the options, and chose "A only
(Recommended)". On values, they chose "5s; CreateOrder 65s (Recommended)": 5s matches
when clients gave up and 0035's connect bound, and 65s sits above order-svc's own 60s
`createTimeout`. The per-method p99 under load was not read before the choice (the box
was unreachable).

## Consequences

- A call whose caller set no deadline ends at 5s with `DEADLINE_EXCEEDED`, which the
  gateway answers as 504.
- `CreateOrder` is never cut before order-svc's own 60s bound, so a saga order-svc would
  finish is not abandoned by the gateway. Its 65s equals the gateway's drain deadline
  ([0016](0016-the-gateway-drains-for-up-to-65s.md)).
- A TS backend keeps running a call the gateway gave up on. Whether A alone shortens a
  backlog like the 2026-11-16 one is for the k3s rerun to show; if it does not, C' is the
  next step.
