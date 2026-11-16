# 0031 — The next work is ranked by what a buyer can hit

**Date:** 2026-10-12
**Status:** accepted
**Arc:** next-work — the open row in the root `CLAUDE.md` register, *Are the deferred stateful-tier phases (b)–(f) the next work?*

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What is the next work, in order, now that 0030 removed the first throughput wall? | 1 the gateway's ~40s loss after a backend crash; 2 checkout tokens the waitroom revokes but nobody checks; 3 one TS service converged to the layout; 4 EKS's door measured. Capacity past both doors waits | The next wall, about 24 a second in Temporal and its Postgres, and the stateful-tier phases (b)–(f) | The ceiling past 24 a second, and which part of Temporal's path sets it, stay unmeasured until a door is planned above about 20 a second |

## Context

The ranking that deferred phases (b)–(f) dissolved on 2026-09-23, and nothing
replaced it. On 2026-10-11 the first wall was the `payment.completed` consumer at
about 9.5 purchases a second, below EKS's door of 10; 0030 removed it, verified on
k3s 2026-10-12. Past it the ceiling is about 24 a second, in Temporal and its
Postgres (`docs/plans/2026-10-09-capacity-on-a-bigger-box.md#what-this-says`).
Both doors sit below that: k3s admits 2 a second, EKS 10.

Checked in the code on 2026-10-12:
- order-svc accepts a checkout token on its signature alone
  (`services/order-svc/internal/order/service/utils.go`, `validateCheckoutToken`);
  the waitroom's `ValidateCheckoutToken`, which reads the blacklist, has no caller.
  A buyer whose chair was freed can check out until the token expires, 5 minutes.
  Inventory still prevents an oversell; the room size of 0025 does not hold.
- `HandleCheckoutCompleted`, `-Failed` and `-Expired` invalidate the token, then write
  back a copy of the session read before it
  (`services/waitroom-svc/internal/service/waitroom_service.go`).
- The waitroom's JWT secret falls back to `"jwt-secret"` when unset
  (`services/waitroom-svc/config/config.go`).
- The gateway's ~40s of failed calls after one refused connection, 2026-09-25, has
  no known cause. A rollout no longer triggers it (0015); a crash, an out-of-memory
  kill or an eviction still can.

## Options

**A. Correctness a buyer can hit first.** The gateway's recovery from a backend
crash, then token revocation, then the TS layout and the EKS door. Leaves the next
ceiling unmeasured.

**B. The next ceiling first.** Tell Temporal's 4 history shards from the commit
flushes on the shared volume, then phases (b)–(f). Needs a bigger-box session and,
for the shard count, a fresh Temporal database: the count is fixed when it is
created. Buys headroom past 24 a second that neither door uses.

**C. Revocation before the gateway.** A known defect with a clear fix ahead of an
investigation with an unknown cause.

## Decision

The architect asked for the ranking, then this order: "yes do it D then C" (D, the
ranking; C, the gateway's recovery), and chose "Accept as ranked" when shown option
A's table, 2026-10-12.

## Consequences

- Phases (b)–(f), the 24-a-second ceiling and `Confirm` contention past 25 a second
  stay open, and return when a door is planned above about 20 a second.
- Each ranked item gets its own plan when the one before it lands.
- The JWT fallback and the stale session write ride with item 2, in the same
  service.

## Outcome

Items 1 and 2 ran in the ranked order:
- 2, token revocation: [0033](0033-order-svc-asks-the-waitroom-whether-a-checkout-token-holds.md),
  verified on k3s 2026-11-15, with the JWT fallback and the stale session write fixed in
  `5db408a`.
- 1, the gateway's recovery: [0035](0035-the-gateway-bounds-a-backend-reconnect.md),
  accepted 2026-11-16. Its run found the gateway's calls carry no deadline, now planned in
  [`2026-11-16-gateway-calls-carry-a-deadline.md`](../plans/2026-11-16-gateway-calls-carry-a-deadline.md),
  unranked.
- 3, event-svc on the layout: planned 2026-11-14 ([0034](0034-event-svc-converges-first-on-its-own.md)), not started.
- 4, EKS's door: no plan yet.

Off the ranking, the late-payment work ([0032](0032-a-payment-after-a-released-hold-gets-its-ticket-if-one-is-left.md))
ran between 2 and 1. The architect accepted the ranking as having held: "yes do what you
propose", 2026-11-16.
