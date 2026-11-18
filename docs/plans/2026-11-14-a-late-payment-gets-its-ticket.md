# A late payment gets its ticket when one is left

**Status: tickets 1–4 DONE 2026-11-14 (`a5842c2`; gates passed on k3s, see [0032](../decisions/0032-a-payment-after-a-released-hold-gets-its-ticket-if-one-is-left.md#outcome)); ticket 5 measured 2026-11-16; the grace is chosen, 30s → [0036](../decisions/0036-a-hold-outlives-the-payment-window-by-30s.md); ticket 6 to do.** Not in
[0031](../decisions/0031-the-next-work-is-ranked-by-what-a-buyer-can-hit.md)'s ranking;
where it slots is the architect's call.

**Question it serves:** what a payment that lands after its hold was released gets
([0024](../decisions/0024-an-unpaid-order-times-out-when-its-hold-expires.md) decision 2,
[0026](../decisions/0026-the-order-contract-tells-the-buyer-what-happened.md)).

**Spec:** none. Derived from an audit of reservation release against the code, 2026-11-14.

## Context

An external review of reservation release was audited against the code (2026-11-14).
Most of its proposals were already built (provider intent expiry at 6 min, ExpireOrder's
TIMEOUT → Release → publish order, per-class batching in the sweeper). It missed the one
real defect:

- **0024's backstop does not run in the normal case.** `ExpireOrder` releases through
  `Release`, which writes the hold `CANCELLED` (`inventory-svc/internal/services/reservation.go`,
  `cancelReservationTx`). Inventory `Confirm` re-acquires only `EXPIRED` holds; `CANCELLED`
  hits `default:` → `ErrStateConflict` → order-svc marks `REFUND_REQUIRED` and pages, even
  with stock free. Only a sweeper-won race re-acquires. The order-side test mocks
  `ConfirmInventory → nil`, so nothing caught it.
- **Owner-cancel opens a guaranteed refund path.** `Cancel` releases the hold but the ZaloPay
  link stays payable for up to 6 min (`CancelPayment` is a stub; the gateway factory routes
  only ZaloPay). A payment after a cancel is refunded by `confirm_order.go`'s
  `OrderStatusCancelled` branch.

**Architect's calls (2026-11-14):** → [0032](../decisions/0032-a-payment-after-a-released-hold-gets-its-ticket-if-one-is-left.md)
1. `ReleaseRequest` carries a reason; a checkout that *ended* (timeout or buyer cancel) is
   written `EXPIRED`, compensation and payment-failure stay `CANCELLED`. Inventory's rule
   is kept: `EXPIRED` = re-acquirable, `CANCELLED` = final.
2. A payment on a buyer-cancelled order is treated like one on a timed-out order:
   confirmed if stock is left, refunded only if not.

Trade-off taken in (1): no new reservation status. `EXPIRED` now also covers a buyer cancel,
meaning "released while its payment link may still be paid". Not taken: a new `ABANDONED`
status, which costs a migration plus a new branch in Confirm, Release and the sweeper.

Outcome: `REFUND_REQUIRED` is left only for real sell-through after a late payment. That is
the case `OrdersNeedingRefund` should page for.

---

## Ticket 1: inventory, a checkout-ended release stays re-acquirable

**Red first (proves the defect on today's API, before any proto change):**
`TestConfirm_AfterReleaseOfTimedOutOrder_Reacquires` in
`inventory-svc/internal/services/reservation_confirm_test.go`: seed a class, Reserve,
Release, Confirm. Today it fails with `ErrStateConflict`. Real Postgres via
`setup_test.go` (`newTestDB`, `reserveSvc`, `seedTicketClass`).

**Change:**
- `proto/inventory.proto`: `enum ReleaseReason { RELEASE_REASON_UNSPECIFIED = 0; RELEASE_REASON_CHECKOUT_ENDED = 1; }`,
  `ReleaseReason reason = 2;` on `ReleaseRequest`. Regenerate everywhere with the
  `proto-change` skill (Go: inventory, order; TS `src/protos/` copies; CI `proto-copies.yml`
  must pass).
- `ReservationService.Release(ctx, oCode, reason)`: `CHECKOUT_ENDED` writes
  `ReservationStatusExpired`, `UNSPECIFIED` writes `Cancelled`. Everything else in
  `cancelReservationTx` (counter decrement, idempotency, CONFIRMED conflict) is unchanged.
- Map the enum in `internal/delivery/grpc`.
- Update the `Confirm` `default:` comment: CANCELLED = released on purpose (compensation,
  failed payment).

**Tests (each red before its code):**
- `TestRelease_CheckoutEnded_MarksExpired`
- `TestRelease_Unspecified_StillMarksCancelled` (guard: compensation stays final)
- `TestConfirm_AfterCheckoutEndedRelease_Reacquires` (the red test above, rewritten to the
  new API)
- `TestConfirm_AfterCheckoutEndedRelease_SoldOut_Conflicts`
- `TestConfirm_AfterCompensationRelease_StillConflicts`
- `TestRelease_CheckoutEnded_AfterSweep_IsNoOp`: the sweeper already wrote EXPIRED, so
  Release changes nothing

**Rollout:** safe in either order. Proto3's default 0 means an old order-svc keeps today's
behaviour, and an old inventory ignores the field.

## Ticket 2: order-svc, ended checkouts release with the reason

**Red first:** `TestExpireOrder_ReleasesAsCheckoutEnded` in `expire_order_test.go`.

**Change:**
- New activity `ReleaseEndedCheckout(orderCode)` in `internal/activities/inventory_activity.go`,
  sending `CHECKOUT_ENDED`. Leave `ReleaseInventory` (compensation) untouched.
  *Why a new activity, not a new arg:* in-flight `ExpireOrder` workflows sit on a 9-min
  timer. A changed activity signature would decode their pending input wrongly.
- `expire_order.go`: switch to the new activity behind `workflow.GetVersion`
  (same pattern as `shortStepsChangeID` in `steps.go`). Old histories keep `ReleaseInventory`.
- `releaseTickets(ctx, code, reason)` in `internal/order/service/utils.go`:
  `Cancel` (`order.go:494`) passes `CHECKOUT_ENDED`; `handlePaymentFailure` (`order.go:432`)
  passes `UNSPECIFIED`.

**Tests:**
- `TestExpireOrder_ReleasesAsCheckoutEnded` (mock expects `ReleaseEndedCheckout`, and
  `ReleaseInventory` not called)
- Existing `expire_order_test.go` cases updated to the new activity name
- `TestCancel_ReleasesAsCheckoutEnded` and `TestHandlePaymentFailure_ReleasesUnspecified`
  (fake inventory client records the request)
- `TestReplay_RecordedHistories` stays green. Record an `expire_order.json` history first
  if none exists, so the version gate is actually exercised.

## Ticket 3: order-svc, a payment on a cancelled order re-acquires

**Red first:** `TestConfirmOrder_APaymentOnACancelledOrderStillGetsItsTicket` in
`confirm_order_test.go`. Today it is marked for refund.

**Change (`internal/workflows/confirm_order.go`):** let `OrderStatusCancelled` through the
gate beside `Pending`/`Timeout`, so it reaches `ConfirmInventory`. `PaymentFailed` keeps the
refund branch. Put it behind `workflow.GetVersion`, because an in-flight ConfirmOrder may have
taken the old branch. Before coding, check that `UpdateOrderStatus` → `COMPLETED` isn't
conditioned on the prior status.

**Tests:**
- `…CancelledOrderStillGetsItsTicket` (ConfirmInventory called, then COMPLETED)
- `…CancelledOrderWhoseStockIsGoneIsRefunded`
- Rewrite `TestConfirmOrder_PaymentOnATerminalOrderIsMarkedForRefund` to use
  `OrderStatusPaymentFailed`, so the refund branch keeps its guard
- Replay test green

**Docs in this ticket:**
- Draft `docs/decisions/0032-a-payment-after-a-cancel-gets-its-ticket-if-one-is-left.md`
  (status proposed; amends 0026's cancel outcome; records both architect calls and the
  EXPIRED-covers-cancel trade-off) with the `decision-records` skill
- Update `services/order-svc/docs/RESERVATION_HOLD.md` backstop paragraph
- Add a CLAUDE.md register row for 0032

## Ticket 4: k3s acceptance, a late payment end to end

Extend `deploy/scripts/gate-checkout-expiry.sh` (already times out buyer A at about 9 min):
- After A reads `CANCELED`, fire `payment-webhook:8080/complete/$A` (the same in-cluster
  adapter `gate1-purchase-flow.sh` uses). Expect A `COMPLETED` through the gateway, the
  reservation `CONFIRMED`, and `sold` +1.
- Buyer C: order, `Cancel`, fire the webhook. Expect `COMPLETED`.
- Expect `OrdersNeedingRefund` metric source at 0, and the waitroom to log no error on
  `checkout.completed` arriving after `checkout.expired`/`checkout.failed` for the same session.

Per the architect-runs-deploys convention: write the run as check-per-step. The architect
runs `make -C deploy k3s-deploy && make -C deploy k3s-gate-checkout-expiry` and pastes the
output. Also re-run `k3s-gate2` and `k3s-gate-sold-out` to show nothing else moved.

### Ticket 4 run, check by check

Prerequisite: the change pushed to `dev` so CI builds `:dev` images, and the tunnel open.

1. `make -C deploy start-ec2-k3s && make -C deploy k3s-kubeconfig`. Check: the box is up and the printed tunnel connects.
2. `make -C deploy k3s-deploy`. Check: `kubectl -n ticketbottle get pods` shows every pod `Running`, with inventory and both order pods on the new image.
3. `make -C deploy k3s-gate2`. Check: `GATE 2 PASSED`.
4. `make -C deploy k3s-gate-sold-out`. Check: it passes.
5. `make -C deploy k3s-gate-checkout-expiry` (about 12 minutes). Check, in order:
   step 4b prints `C: cancelled, paid, COMPLETED`; step 6 shows A's reservation
   `EXPIRED`; step 8 prints `reserved/sold 0/3` and the run ends `CHECKOUT-EXPIRY GATE PASSED`.
6. `kubectl -n ticketbottle logs deploy/waitroom-service --since=20m | grep -i error`. Check: nothing about A's or C's session.

When it passes, flip 0032 to `accepted` with an Outcome citing the run.

## Ticket 5 (spike, not TDD, after 1–4): size the grace from measurement

The provider closes at 6 min and the hold lives to 9 min, so every abandoned hold keeps its
stock for 3 dead minutes. Measure the webhook → outbox → Kafka → ConfirmOrder lag p99 on
k3s, propose a `ReservationHoldGrace`, and leave the change itself to the architect.
(The `PaymentTimeout` comment was corrected with tickets 1–3.)

**Instrument:** `purchase.js` records `tb_confirm_lag_s`, from the webhook's answer to
the buyer seeing `COMPLETED`, at the 2s poll's resolution; against a 3-minute grace
that error does not matter. It covers the leg at the k3s door, not a stopped relay or
consumer: a backlog no grace covers is 0017's page.

Run in the same box session as the gateway plan's Task 3, after its gate2:

```bash
make -C deploy k3s-load                     # 3m, 20 buyers, door at 2 a second
kubectl -n ticketbottle logs job/k6-load | grep -A8 '"tb_confirm_lag_s"'
kubectl -n ticketbottle logs job/k6-load | grep -A3 '"tb_orders_completed"'
```

- [ ] Record p99, max and the completed count under a *Results* section.
- [ ] Propose a grace of p99 times a margin, with the cost each way: shorter frees stock
  sooner; too short turns a late confirm into `REFUND_REQUIRED` and a page.

### Ticket 5 results

**2026-11-16, `sha-4355dd3`, `k3s-load` (3m, 20 buyers, door at 2 a second).** 372
purchases completed (1.96 a second). Webhook → `COMPLETED` seen: median 2.10s, p95 2.30s,
p99 2.47s, max 2.52s. A buyer polls every 2s, so the true lag is under about 2.1s.

**Proposal:** a grace of 30s instead of 3 minutes: over ten times the measured p99, and
six relay safety polls (`RELAY_SAFETY_POLL_MS`, 5s) for a missed NOTIFY. An abandoned
hold then frees its stock 2.5 minutes sooner. The cost runs the other way: a confirm held
up past 30s by a relay or consumer stall becomes `REFUND_REQUIRED` and pages, where 3
minutes would have absorbed it.

## Ticket 6: the grace to 30s → [0036](../decisions/0036-a-hold-outlives-the-payment-window-by-30s.md)

One constant, `ReservationHoldGrace` in `services/order-svc/internal/workflows/shared.go`,
sets the hold, `ExpireOrder`'s delay and so the chair: 9m becomes 6m30s.

- [x] **Replace the test that forbids it.** `TestReservationHoldGrace_CoversWorkerInterval`
  requires the grace to exceed inventory's 60s sweep. The sweep frees only holds already
  past `expires_at` (`selectAndExpireBatch`, `expires_at <= now`), so it can never take a
  hold inside its grace; the test guards nothing. Its replacement states 0036's rule: the
  grace is at least 30s, ten times the measured confirm-lag p99, citing the plan's
  *Ticket 5 results*. Set the constant to 29s and see it red.
- [x] **Set `ReservationHoldGrace = 30 * time.Second`.** `CheckoutLifetime` and
  `reservationExpiry` follow. The comment above it loses the sweep sentence.
- [x] **Replay.** `go test ./internal/workflows/...`: the recorded histories still replay.
  `CreateOrder` passes the expiry as activity input and `ExpireOrder`'s delay is set
  outside the workflow, so neither is a command replay checks; the run confirms it.
- [x] **The facts that say 9 minutes**, each in its one home:
  `services/order-svc/docs/RESERVATION_HOLD.md` (the formula),
  `services/order-svc/CLAUDE.md` (`CheckoutLifetime`, 9m), `services/inventory-svc/CLAUDE.md`
  (hold for ~9 min), `docs/RUNBOOK.md` (a hold expires 9 minutes after),
  `docs/design/admission-sizing.md` (released at about 9m). Decision records keep their
  9 minutes: they record what was true when made.
- [x] **The k3s gate's window.** `deploy/scripts/gate-checkout-expiry.sh` step 6 wants
  510–660s for a 540s hold; the hold is now 390s, so 360–510s, and the wait in step 5
  shortens to 9m. Its header says ~9m.
- [ ] **On k3s**, after CI builds the commit: `REF=sha-<full sha> make -C deploy k3s-deploy`,
  then `k3s-gate-checkout-expiry` (A expires at about 390s; A and C still get their
  tickets) and `k3s-gate2`. Accept 0036 with the run.

## Out of scope (audited, not worth building now)

The sweeper as a margin-delayed safety net with an alert; bucketing the stock row (no
evidence that releases bind); provider-side cancel (ZaloPay has no cancel API, so verify before
reviving); removing the dead PayOS gateway; the `PaymentCancelled` misroute.

## Verification

- `cd services/inventory-svc && docker compose -f docker-compose.dev.yml up -d && go test ./internal/services/...`
- `cd services/order-svc && go test ./internal/workflows/... ./internal/order/...`
- Proto: regenerate, then the proto-copies check stays clean (`git diff --exit-code` on stubs)
- Every new test is seen red before its code, then green. Revert the one-line fix to confirm
  it goes red again (the mutation check from plans-need-compiling memory)
- Ticket 4's k3s gate output is the end-to-end proof
