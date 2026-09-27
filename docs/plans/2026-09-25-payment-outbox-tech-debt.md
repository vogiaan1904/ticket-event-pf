# Payment outbox tech debt — plan

**Status: item 1 BUILT 2026-09-27, its k3s check not yet run; item 2 DEFERRED.**
Written 2026-09-25. Two defects in the payment event path, found while tracing it.
Item 1 could strand a charged buyer without an alert — decided as
[0017](../decisions/0017-an-exhausted-payment-event-pages.md); item 2 is latent
until `CancelPaymentIntent` is wired.

**Goal:** Make an outbox event that can never publish visible on the cluster, and
make the cancel event route and parse correctly before anything emits it.

The mechanism this plan touches is owned by `.claude/skills/outbox-relay/SKILL.md`;
read that first.

## 1 — A poison outbox event is silent on the cluster

**What happens.** A row that fails to publish gets `retryCount + 1`
(`services/payment-svc/lambdas/common/db/outbox.repo.ts:36-42`). At
`OUTBOX_MAX_RETRIES` (5, `services/payment-svc/outbox-relay/src/db.ts:12`) the claim
query stops selecting it (`outbox.repo.ts:24`). From then on nothing reads it again.

**Why nothing catches it.** The exit ramp is the `outbox-cleanup` Lambda, and no
Lambda runs on k3s or EKS (`.claude/skills/deployment-architecture/references/k3s-ec2.md:178`).
It has two jobs, and neither happens on the cluster:

| Job | In `services/payment-svc/lambdas/outbox-cleanup/handlers/cleanup.handler.ts` | On the cluster |
|---|---|---|
| Route exhausted rows to the SQS DLQ, emit `OutboxFailedEvents` | `routeExhaustedEvents` | Rows sit forever; no alarm |
| Delete published rows past retention | `deleteOldEvents` | `outbox` grows without bound |

**Impact.** The buyer was charged, `payment.completed` never reaches Kafka, and
`ConfirmOrder` never runs. The alerts miss it:

- `OrdersNeedingRefund` counts `tb_order_refund_required_total`, which only a
  *failed* confirm raises. No confirm, no count.
- `OutboxBacklogGrowing` counts every `publishedAt IS NULL` row
  (`services/payment-svc/outbox-relay/src/metrics.ts:31-38`), exhausted ones included.
  One poison row never reaches 50. Fifty accumulated ones fire it for good, and its
  runbook section diagnoses a stalled relay, which is the wrong cause.

The unbounded table is minor: the partial index `idx_outbox_unpublished` keeps the
relay's query on the unpublished set, so it costs disk, not latency.

**Options** — the architect's call:

| Option | Cost |
|---|---|
| **(a)** A `tb_outbox_exhausted_rows` gauge beside `tb_outbox_pending_rows` in `metrics.ts`, an alert on `> 0`, a runbook section; exclude exhausted rows from the pending gauge | Small. Recovery stays manual: inspect `lastError`, fix, reset `retryCount` |
| (b) Port `outbox-cleanup` to a k8s CronJob | Needs a DLQ the cluster owns (SQS is AWS-only), so a new topic or table and its consumer |
| (c) Deploy the Lambda path to AWS | Reverses "Lambda is optional future work" and couples the cluster to AWS |

Recommended: **(a)**. It fixes the silence, which is the harm. The DLQ adds a
replay path that nothing yet needs. Retention can follow as a one-statement CronJob
if disk ever matters. Chosen 2026-09-27 → [0017](../decisions/0017-an-exhausted-payment-event-pages.md).

**Done when** a check that can go red passes on k3s: insert an outbox row with
`retryCount = 5`, the new alert fires, and `OutboxBacklogGrowing` does not.

### Measured 2026-09-27: how long a Kafka outage takes to exhaust a row

The cap counts attempts, not time, so any Kafka outage — not only a malformed event
— exhausts the rows in flight if it lasts long enough. On k3s, build `sha-e3f0b51`
(relay code unchanged since `d238fb0`): Redpanda scaled to 0, one synthetic
`PAYMENT_COMPLETED` row inserted, `retryCount` read every second. The row was deleted
before the broker returned.

| retryCount | Seconds after insert | Since the previous |
|---|---|---|
| 1 | 93 | 93 |
| 2 | 226 | 133 |
| 3 | 339 | 113 |
| 4 | 402 | 63 |
| 5 | **789** | 387 |

Every failure was `ECONNREFUSED`, 3 of 3 attempts; the relay did not restart. One
`producer.send` took 14s to 167s to give up, inside kafkajs's own retries
(`lambdas/common/kafka/producer.ts`), and that — not the 5s safety poll — sets the pace.

For comparison, `rollout restart statefulset/redpanda` was Ready in 9.1s, and a scale
from 0 in 10.2s. After the outage `make -C deploy k3s-gate2` passed: the producer
reconnects on its own.

**What it means:**

- **A broker restart does not strand rows.** 10s is shorter than the fastest failed
  send seen (14s), so a restart is absorbed inside the first attempt. Inferred from
  the timings, not measured with a row in flight during a restart.
- **Exhaustion needs a broker that stays down — about 13 minutes** in one run. The
  gaps vary from 63s to 387s, so 5 to 30 minutes is plausible.
- **Load does not speed it up.** The drain scheduler serializes wake-ups, so a
  NOTIFY only closes the ≤5s poll gap. Each wake-up retries the same oldest batch of
  100, so a longer outage exhausts 100 rows per ~13 minutes (from the code).
- **Recovery after (a) fires** is one statement once the broker is back:
  `UPDATE outbox SET "retryCount" = 0 WHERE "publishedAt" IS NULL AND "retryCount" >= 5`.
  An order whose hold was swept meanwhile then becomes `REFUND_REQUIRED` and pages
  through `OrdersNeedingRefund`.

So **(a) is enough on its own**: a time-based cap would only change an outage longer
than ~13 minutes, which (a) makes visible and one statement recovers.

## 2 — The cancel event would misroute and not parse

`cancelPayment` writes eventType `'PaymentCancelled'`
(`services/payment-svc/src/modules/payment/payment.service.ts:108`). The relay's
`topicFor` matches `EventType.PAYMENT_CANCELLED`, the string `'PAYMENT_CANCELLED'`
(`services/payment-svc/outbox-relay/src/kafka.ts:14`), so the row falls to the default:
`payment.failed` (`kafka.ts:17`). Order consumes that topic and reads `order_code`
(`services/order-svc/internal/order/delivery/kafka/presenter.go`); the cancel payload
sends `orderCode`.

**Latent:** nothing calls `cancelPayment`, and `CancelPaymentIntent` has no handler
(`services/payment-svc/CLAUDE.md`, *Layout*).

**Fix, when `CancelPaymentIntent` is wired:** write `EventType.PAYMENT_CANCELLED`
with the snake_case payload the other events use, and decide who consumes
`payment.cancelled` — today nothing does.

**Open trade-off in `topicFor`'s default.** An unknown eventType goes to
`payment.failed`, so a typo becomes a wrong message instead of an error. Throwing
instead sends the row through `markFailed` to exhaustion, which is item 1's silent
path until (a) lands. Decide it after item 1.

## Out of scope

- **The cluster's webhook verifies no provider signature.** It is a simulated
  provider by design; the owner is `.claude/skills/deployment-architecture/references/k3s-ec2.md`.

## Results

### Item 1 — built 2026-09-27

| Piece | Where |
|---|---|
| `countUnpublished` — splits unpublished rows at the claim's own cut-off | `services/payment-svc/lambdas/common/db/outbox.repo.ts` |
| `OUTBOX_MAX_RETRIES` read once, for the claim and the gauge | `services/payment-svc/outbox-relay/src/config.ts` |
| `tb_outbox_exhausted_rows`; `tb_outbox_pending_rows` no longer counts exhausted rows | `services/payment-svc/outbox-relay/src/metrics.ts` |
| `OutboxEventsExhausted`, page, no `for` | `deploy/helm/ticketbottle/templates/apps/prometheusrule.yaml` |
| Recovery | `docs/RUNBOOK.md#outboxeventsexhausted` |

**Evidence so far:**

- Two database tests, run against a Postgres carrying every payment migration: the
  split at the cut-off, and pending equal to what `claimBatch` can claim. Red on
  `<=` for pending, on counting published rows, and on moving the claim's cut-off.
- Two relay tests: the gauges set apart, and the count taken at the configured
  cut-off (mocked to 7, so a hard-coded 5 fails). Red on swapping the gauges, on a
  hard-coded cut-off, and on never setting the exhausted gauge.
- The relay image built as CI builds it, with kysely 0.27.6; its compiled
  `countUnpublished` returned `{"pending":2,"exhausted":2}` against rows at retry
  counts 0, 4, 5 and 6 plus one published row.
- `promtool test rules` on the rendered rule: silent at 0; fires on one row; still
  firing when the relay pod is replaced and the new one reports 0 for 30s; clears
  5 minutes after the reset. A bare `max(tb_outbox_exhausted_rows) > 0` fails the
  replacement case.
- `assert-render.sh` passes with the regenerated golden; the only drift was the rule.

**Not yet run:** the *Done when* check on k3s. It needs this build in ECR.
