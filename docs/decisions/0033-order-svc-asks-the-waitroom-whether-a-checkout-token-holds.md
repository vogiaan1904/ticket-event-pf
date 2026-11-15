# 0033 — order-svc asks the waitroom whether a checkout token still holds

**Date:** 2026-11-14
**Status:** accepted
**Arc:** token-revocation — [plan](../plans/2026-11-14-checkout-token-revocation.md)
**Where it lives:** `proto/waitroom.proto`, `services/order-svc/internal/order/service/utils.go` (`validateCheckoutToken`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Where is a revoked checkout token refused? | A waitroom `ValidateCheckoutToken` RPC, called by order-svc before the purchase slot; fails closed | order-svc keeping the session-keyed slot so a token buys once; the same RPC failing open | A synchronous hop on every waitroom checkout, and no checkout on a waitroom event while the waitroom is down |

## Context

The waitroom revokes a token when its checkout completes, fails or expires, and has
a validator that reads the revocation. Nothing calls it: order-svc checks the
signature and claims only. For up to 5 minutes (0027) a buyer can order again with
a spent token, past the per-order limit (0021) and the room size (0025). Inventory
still prevents an oversell.

## Options

**Waitroom RPC, fail closed** (recommended). The waitroom stays the one owner of
session state. Costs a hop per checkout and makes the waitroom a hard dependency of
checkout on waitroom events.

**One order per admission, in order-svc.** Keep the session-keyed purchase slot after
a completed order. No new call. Costs the slot's rule that a failed order frees it
for a retry (`services/order-svc/docs/PURCHASE_SLOT.md`), and covers only what order-svc itself saw end.

**Waitroom RPC, fail open.** As the first, but an unreachable waitroom lets the
signature decide, as 0023 does for its inventory check. Costs reopening the hole for
the length of any waitroom outage.

## Decision

The architect was given the three options above, each with its cost, and answered
"Waitroom RPC (Recommended)", 2026-11-14.

Four choices came with the plan and were not put as separate calls: the waitroom
alone verifies the signature, so order-svc drops `JWT_SECRET`; a refused token is
`UNAUTHENTICATED` (`ORD016`); an unreachable waitroom is `UNAVAILABLE`; a retry after a
completed checkout gets 401, not the existing order. The plan names each with its cost.

## Consequences

- A waitroom outage stops checkout on waitroom events; events without a waitroom
  are unaffected.
- One fewer workload holds the key that mints a checkout.
- A token revoked by an event still in Kafka buys until the waitroom consumes it.

## Outcome

Built test-first in `8c5ba2e` (the RPC, every refusal `UNAUTHENTICATED` `WTR013`),
`bbb7270` (order-svc asks before the purchase slot; `ORD016` on a refusal, `ORD021`
`UNAVAILABLE` on an unreachable waitroom; its JWT verifier and `JWT_SECRET` deleted)
and `5db408a` (the handlers revoke last; `JWT_SECRET` has no default).

Run on k3s, `gate-token-revoked` (`f78287c`):
- 2026-11-14, on `a5842c2`, before the change: red, "A's spent token answered 201".
  That build has no caller of the RPC, so it is Ticket 2's call reverted.
- 2026-11-15, on `sha-83537aa`: green. A completed and B cancelled; each session read
  ended 1s later, and each token's reuse answered 401 `ORD016`. `gate2` and
  `gate-orders` still pass.

The hop's cost, from Prometheus after the gates: 6 calls (4 `OK`, the 2 refusals
`UNAUTHENTICATED`), a mean of 0.49 ms, all under the smallest bucket of 5 ms. The
Kafka window of the third consequence was the 1s before each session read ended.
