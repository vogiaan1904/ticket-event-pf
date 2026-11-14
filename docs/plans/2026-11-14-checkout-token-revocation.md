# Checkout-token revocation

**Status: tickets 1–3 DONE 2026-11-14 (`8c5ba2e`, `bbb7270`, `5db408a`); ticket 4 (k3s) open.** Item 2 of
[0031](../decisions/0031-the-next-work-is-ranked-by-what-a-buyer-can-hit.md). The
check's place is decided in
[0033](../decisions/0033-order-svc-asks-the-waitroom-whether-a-checkout-token-holds.md).

**Question it serves:** a checkout token the waitroom has revoked still buys.

**Spec:** none. Derived from 0031's code check of 2026-10-12, re-verified 2026-11-14.

## What is true today

- order-svc accepts a token on its signature and claims alone
  (`services/order-svc/internal/order/service/utils.go:30`, `validateCheckoutToken`).
- The waitroom revokes on `checkout.completed`, `-failed` and `-expired`, writing a
  blacklist key and setting the session's status. Its `ValidateCheckoutToken`
  (`services/waitroom-svc/internal/service/session_service.go`) reads both, and has
  no caller: `proto/waitroom.proto` has no RPC for it.
- **What a buyer can do:** after a completed or failed checkout, place another order
  with the same token until it expires (5 minutes, 0027). Each order is within the
  per-order limit (0021), but their sum is not, and the room size (0025) does not
  bound buyers holding stock. Inventory still prevents an oversell.
- `HandleCheckoutCompleted`, `-Failed` and `-Expired` invalidate the token, then
  write back the session they read before it, clearing `TokenInvalidated`
  (`services/waitroom-svc/internal/service/waitroom_service.go`). The blacklist key
  and the status survive the write, so today the stale field does not reopen a token;
  it is a trap for the next reader of that field.
- The waitroom's JWT secret falls back to `"jwt-secret"`
  (`services/waitroom-svc/config/config.go:112`). Its production guard compares
  against a different string, `"your-super-secret-key-change-in-production"`, so it
  never fires, and the chart sets `ENV: development` anyway.

## Decided

- **The waitroom answers, order-svc asks** — a `ValidateCheckoutToken` RPC, called
  before the purchase slot is claimed; unreachable waitroom fails closed →
  [0033](../decisions/0033-order-svc-asks-the-waitroom-whether-a-checkout-token-holds.md).

## Choices that came with this plan

Not put to the architect as separate calls; each is named so it can be overturned
before Ticket 2.

| Choice | Taken | Not taken | Cost |
|---|---|---|---|
| Who verifies the signature | The waitroom only; order-svc drops its JWT verifier and `JWT_SECRET` | order-svc verifies first, then asks | A malformed token costs a hop; in return one fewer workload holds a key that mints a checkout for any user (`deploy/helm/ticketbottle/templates/apps/secrets.yaml`) |
| Code for a revoked, expired or mismatched token | `UNAUTHENTICATED`, the existing `ORD016` (401) | `FAILED_PRECONDITION` (409) | A client cannot tell "your checkout ended" from "bad token"; both mean rejoin the queue |
| Code for an unreachable waitroom | `UNAVAILABLE` (503), a new `ORD0xx` | — | Checkout on a waitroom event stops while the waitroom is down |
| A retried `CreateOrder` after the checkout completed | 401, not the existing order | Look up the slot first, then validate | A buyer who retries after paying sees 401 instead of their order; the order is unchanged and readable through `GET /api/orders/:code` |

## Global constraints

- One contract change, regenerated in every consumer (`proto-change` skill); CI's
  `proto-copies.yml` fails on any stale copy, the gateway's included.
- The comment budget in the root `CLAUDE.md` holds.
- Go: the service tests use fakes, as `services/order-svc/internal/order/service/cancel_test.go`
  does for `invSvc`.

## Ticket 1: the RPC in the waitroom (TDD)

- [ ] `proto/waitroom.proto`: `rpc ValidateCheckoutToken(ValidateCheckoutTokenRequest)
  returns (ValidateCheckoutTokenResponse)`; request `token`, `user_id`, `event_id`;
  response `session_id`. Regenerate everywhere.
- [ ] Red first, in `session_service` tests: a token whose session is `COMPLETED`,
  `FAILED` or `EXPIRED`; a blacklisted token; a token for another user or event;
  an expired one. Each is refused. An `ADMITTED` session's token returns its id.
- [ ] `ValidateCheckoutToken` checks the claims' `user_id` and `event_id` against the
  request, and returns the session id.
- [ ] `internal/delivery/grpc`: the handler, and `UNAUTHENTICATED` (`WTR013`) for every
  token refusal; `UNAVAILABLE` (`WTR014`) when Redis cannot be read, since a
  dependency is down, not a bug.

## Ticket 2: order-svc asks (TDD)

- [ ] A waitroom client in `pkg/grpc/waitroom` like `NewInventoryClient`; dialled in
  `cmd/api/main.go`; `WAITROOM_SERVICE_ADDR: waitroom-service:50056` in `order-config`.
- [ ] Red first, with a fake client: `UNAUTHENTICATED` → `ErrInvalidCheckoutToken`;
  `UNAVAILABLE` or `DEADLINE_EXCEEDED` → `ErrWaitroomUnavailable` (`UNAVAILABLE`);
  any other code is returned as is; no purchase slot is claimed in any refusal.
- [ ] `validateCheckoutToken` calls the client; delete the local JWT verifier, its
  config and order-svc's `JWT_SECRET` from `secrets.yaml`.

## Ticket 3: the waitroom's own loose ends

- [ ] The three `HandleCheckout*` write the status first, then invalidate, so the
  invalidation is the last write. Red first: after `HandleCheckoutCompleted`, the
  stored session reads `TokenInvalidated: true`.
- [ ] No default for `JWT_SECRET`: `Validate` refuses an empty secret in every
  environment. The chart already requires it; a native run sets it.

## Ticket 4: prove it on k3s

The architect runs it and pastes the output.

- [ ] `make -C deploy k3s-deploy` at the ticket's `sha-` tag.
- [x] A new `deploy/scripts/gate-token-revoked.sh`, with a `k3s-gate-token-revoked`
  target: buy through to `COMPLETED`; once the waitroom answers the session ended,
  `POST /api/orders` with the same token → 401 `ORD016`. Then a second buyer orders
  and cancels, and the same. A cancel, not a chair expiry: the token expires with
  the chair (0027), so an expiry case would pass on the JWT's `exp` alone.
- [ ] Mutation check: revert Ticket 2's call; the gate goes red.
- [ ] `make -C deploy k3s-gate2` and `k3s-gate-orders` still green.
- [ ] The RPC's cost, from Prometheus after the gates:
  `histogram_quantile(0.99, sum by (le) (rate(tb_grpc_request_duration_seconds_bucket{service="waitroom",method=~".*ValidateCheckoutToken"}[10m])))`.

## Ticket 5: record

- [ ] 0033 gains its Outcome; the register row and `services/waitroom-svc/CLAUDE.md`
  name the RPC; the `secrets.yaml` header no longer says order-service verifies by
  signature alone.

## Not covered

A token revoked by an event that has not yet reached the waitroom still buys: the
window is Kafka's lag from `checkout.completed`, measured in Ticket 4 if it shows.

## Results

Not yet run.
