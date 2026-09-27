# 0018 — Short saga steps run as local activities

**Date:** 2026-09-27
**Status:** proposed
**Arc:** saga-orchestration-cost — [plan](../plans/2026-09-27-saga-short-steps-local.md)
**Where it lives:** `services/order-svc/internal/workflows/steps.go` (`executeShortStep`), `services/order-svc/internal/workflows/options.go` (`shortStepsChangeID`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Which saga steps pay a Temporal task-queue hand-off? | Six short, idempotent steps run as local activities behind a workflow version; reserve, confirm and the payment intent stay remote | Merging steps into fewer regular activities | A local step re-runs when the workflow task that ran it fails, so every such step must be idempotent, and its workflow tests run real activities over fakes |

## Context

On the k3s box a purchase costs about one core-second, and the box saturates at
ten buyers. Temporal spends 0.24 core-seconds of that and Postgres, mostly
Temporal's database, another 0.19; the seven services together spend about as
much as Temporal alone. Each purchase is two workflows, nine activities and 64 history
events, and every activity is a round trip through Temporal's task queue with
writes behind it. The opening burst of an on-sale queues on exactly those
dispatches: `docs/plans/2026-09-27-checkout-latency-decomposition.md`.

Six of the nine steps are short calls to the service's own stores: `GetOrder`,
`CreateOrder`, `CreateOrderItems`, `UpdateOrderStatus`, `ReleasePurchaseSlot`,
`PublishCheckoutCompleted`. A local activity runs such a step inside the
workflow worker and records one marker, with no task-queue round trip.

## Options

**Local activities for the six short steps.** Nine dispatched activities per
purchase become three. The retry policy per step is unchanged. It costs four
things: a local step re-runs if its workflow task fails after it completed, so
each must be idempotent (`CreateOrderItems` was not, and is fixed first); the
command sequence changes, so the change is gated by `workflow.GetVersion` and
proven by replaying recorded histories; the SDK's test suite cannot match a
mock to a local activity registered on a struct, so the local path is tested
with real activities over fakes; and a local step appears in history as a
marker, not as a pending activity.

**Merge steps into fewer regular activities** — `CreateOrder` with
`CreateOrderItems`, and the tail of `ConfirmOrder` into one. Mocks and
visibility stay as they are, but nine activities become six, not three, and
the merged activities change retry and error granularity: the confirm tail
deliberately swallows publish and slot-release failures step by step, and a
merged activity would have to reproduce that inside itself.

**A separate database for Temporal.** Moves Temporal's load off the app's
Postgres without reducing it, and is infrastructure growth the 2026-09-16 scope
decision stopped.

**Leave it.** The cost per purchase stays where it was measured.

## Decision

Proposed, not yet made. The architect asked for the plan and this record: "yes
do it" (2026-09-27), in reply to the proposal to write up the local-activity
split with a before/after measurement.

The split in that proposal also merged `CreateOrder` with `CreateOrderItems`.
This record drops the merge — the agent's revision, not yet reviewed: a local
activity removes the hand-off between the two without changing their
compensation.

## Consequences

- Fewer dispatches per purchase, and fewer tasks in the queue a burst waits on.
- Any step routed through `executeShortStep` must be safe to run twice. A step
  that writes must write the same thing on every run; that is what forced
  deterministic order-item IDs.
- The `DefaultVersion` branch stays until no saga started before the deploy can
  replay; removing it is a later change with its own replay check.
- `CreateOrderSlotBudget` is still an upper bound: a local step's retry budget,
  at 5s per attempt, is shorter than the regular one it was derived from.
