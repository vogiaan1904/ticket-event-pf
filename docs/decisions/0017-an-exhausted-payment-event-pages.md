# 0017 — A payment event that stops retrying pages; recovery stays manual

**Date:** 2026-09-27
**Status:** accepted
**Arc:** payment-outbox — [plan](../plans/2026-09-25-payment-outbox-tech-debt.md)
**Where it lives:** `services/payment-svc/lambdas/common/db/outbox.repo.ts` (`countUnpublished`), `services/payment-svc/outbox-relay/src/metrics.ts`, `deploy/helm/ticketbottle/templates/apps/prometheusrule.yaml` (`OutboxEventsExhausted`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How does a payment event that can never publish become visible on the cluster? | A gauge of rows past the retry cap, a page on any, recovery by one hand-run `UPDATE` | Also capping retries by time, so a long Kafka outage strands nothing | A Kafka outage longer than ~13 minutes strands every row it touches until someone resets them |

## Context

The relay gives each outbox row five publish attempts (`OUTBOX_MAX_RETRIES`); past
that, the claim query never selects the row again. The exit ramp — an SQS dead-letter
queue and a CloudWatch alarm — lives in the `outbox-cleanup` Lambda, and no Lambda
runs on k3s or EKS. So on the cluster the buyer is charged, `payment.completed` never
reaches Kafka, the order stays `PENDING`, and its hold is released nine minutes in.

No alert saw it. `OrdersNeedingRefund` counts only confirms that failed, and here no
confirm runs. `OutboxBacklogGrowing` counted exhausted rows as backlog: silent below
50, then firing for good with a runbook that blames a stalled relay.

The cap counts attempts, not time, so any Kafka outage exhausts the rows in flight
if it lasts long enough — not only a malformed event. On k3s on 2026-09-27 one row
took **789s (13.1 minutes)** of broker outage to exhaust, while a broker restart was
Ready again in 9–10s. Method and numbers:
[the plan](../plans/2026-09-25-payment-outbox-tech-debt.md#measured-2026-09-27-how-long-a-kafka-outage-takes-to-exhaust-a-row).

## Options

**(a) A gauge, a page and a runbook.** `tb_outbox_exhausted_rows` counts rows past
the cap, an alert pages on any, and the pending gauge stops counting them. Small.
Recovery is a person resetting `retryCount` once the cause is gone.

**(a) plus a time-based cap.** A row is abandoned only after failing for some
duration, through a `nextAttemptAt` column or the row's age. A temporary outage then
strands nothing. Costs a migration and more logic on the money path, and by the
measurement it changes only outages longer than ~13 minutes.

**(b) Port `outbox-cleanup` to a CronJob.** Needs a dead-letter queue the cluster
owns, since SQS is AWS-only: a new topic or table and its consumer, for a replay path
nothing yet needs.

**(c) Deploy the Lambda path to AWS.** Reverses "Lambda is optional future work" and
couples the cluster to AWS.

## Decision

"go with (a)" — the architect, 2026-09-27, choosing between (a) alone and (a) with a
time-based cap, after the k3s measurement. (b) and (c) were in the plan's options
table and were not put again.

## Consequences

The page fires within one refresh and one evaluation of a row exhausting. The rule
has no `for`, since a row leaves this state only by hand; `max_over_time(...[5m])`
bridges the 0 a restarted relay reports before its first refresh, so a relay rollout
does not clear and re-fire it. It clears five minutes after the reset.

`OutboxBacklogGrowing` now reads only rows the relay will still claim, so its runbook's
diagnosis — the relay is not draining — holds whenever it fires.

The claim and the gauge must cut at the same count. `OUTBOX_MAX_RETRIES` is read once,
in `outbox-relay/src/config.ts`, and a database test holds the pending count equal to
what `claimBatch` can claim.

Recovery is manual, and the refund path behind it is too: a reset row whose stock
was resold confirms as `REFUND_REQUIRED`, and nothing consumes `order.refund_required`.
If outages past ~13 minutes become routine, or one incident strands more rows than a
person can reason about, the time-based cap is the next step. Retention is not
addressed: published rows accumulate on the cluster, which costs disk, not latency.

## Outcome

`11df387`. Two database tests hold the split at the retry cut-off and the pending count
equal to what `claimBatch` can claim; two relay tests hold the gauges apart and the count
at the configured cut-off. Six mutations each turned a test red. `promtool` showed the
rule firing on one row, holding through a relay replacement that reports 0 for 30s, and
clearing five minutes after the reset; a bare `max(...) > 0` failed the replacement case.

On k3s at `sha-11df387`, 2026-09-27:

- **Detection.** 60 rows at `retryCount = 5` fired `OutboxEventsExhausted` 32s after the
  insert, reaching Alertmanager with the summary "60 payment events stopped retrying".
  `tb_outbox_pending_rows` read 0 throughout; before this change it would have read 60.
- **A real order, stranded and recovered.** With the relay stopped, a purchase through the
  gateway left `TB-GATE1-20260927-54GFXBKW` paid and `PENDING`. Its outbox row set to
  `retryCount = 5` stayed unpublished with the relay back up, and the order stayed
  `PENDING`. The runbook's `UPDATE`, run as written, completed the order 7.0s later.
- **Clearing.** The alert cleared about 4.5 minutes after the gauge returned to 0.
