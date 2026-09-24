# Architect calls from the system map — plan

**Status: carried out 2026-09-24; closes when CI has run on it.** The *Found, not fixed* list of
`docs/plans/2026-09-23-system-map-skill.md`, put to the architect and carried out.
Decisions: [0013](../decisions/0013-the-gateway-throttles-sign-in-not-purchase.md),
[0014](../decisions/0014-ts-structure-follows-the-june-layout-rule.md); 0012 made the
architect's own.

## The calls

| Item | Call | Done | Evidence |
|---|---|---|---|
| `OrdersNeedingRefund` could not fire | Fix (implementation) | `tb_order_refund_required_total`, counted where the activity writes `REFUND_REQUIRED`, unlabelled so it exports 0; the rule loses its `for: 10m`; the runbook reads `order-consumer` | Test red, then green; two mutations each red. `promtool`: the old rule never fired on one refund, the new one fires at 6m and clears at 16m, a series born at 1 never fires |
| Cluster `payment-webhook` guard | Fix | The Lambda's `status = 'PENDING'` compare-and-set; a repeat answers 200, counted `duplicate` | Against payment-svc's migrations: two calls wrote two outbox rows before, one after |
| `MODELS.md` | Delete | Deleted; `internal/models/` and `ddl.go` own the schema | — |
| Lambdas README, order `SYSTEM.md` | Rewrite the wrong parts | README cut to what exists; `SYSTEM.md` status flow from the code | Each claim checked against the code first |
| Gateway rate limit and headers | → [0013](../decisions/0013-the-gateway-throttles-sign-in-not-purchase.md) | Helmet; `AuthRateLimitGuard` on sign-in and sign-up; `APP_TRUST_PROXY_HOPS` 0 on k3s, 1 on EKS | Seven tests; three mutations each red; the built gateway booted: 503, 503, 503, 429, labelled `RESOURCE_EXHAUSTED` under `POST /api/auth/signin` |
| TS layout reference | → [0014](../decisions/0014-ts-structure-follows-the-june-layout-rule.md) | `docs/design/ts-layout.md`; root `CLAUDE.md`, the `add-service` skill and the gateway's `src/CLAUDE.md` point at it | No code moved |
| Record 0012 | Owned | Ownership recorded, delegation caveat removed | — |

## Corrected from the previous plan

The cluster webhook's missing guard did not produce `REFUND_REQUIRED`, as that plan
said. Nothing moves a payment out of `PENDING` except completing it, so a late
`/complete` behaves the same with or without the guard; a repeated one wrote a second
outbox row and a duplicate `payment.completed`, which `ConfirmOrder` ignores for an
order already `COMPLETED`.

## Found, not fixed

- **The chart's CORS allowlist is never read.** The chart sets `CORS_ORIGINS`; the
  gateway reads `APP_CORS_ORIGINS`, so every cluster allows only
  `http://localhost:3000`. The chart's value, `'["*"]'`, is a JSON array the code
  would split into one literal origin, so renaming the key alone breaks CORS. No
  browser client exists yet, so nothing fails today.
- **The gateway ignores `SIGTERM`.** `src/main.ts` handles it only to close the
  metrics server, which replaces Node's default exit; each pod runs until the 30-second
  grace period kills it, on every rollout and scale-in.
- **`PAYMENT_TIMEOUT_SECONDS` is loaded and never read** by order-svc; the payment
  window is the `PaymentTimeout` constant.
