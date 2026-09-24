# Runbook — alerts

One section per rule in `deploy/helm/ticketbottle/templates/apps/prometheusrule.yaml`. Each alert's
`runbook` annotation points at its anchor here.

**Before running anything below.** Point kubectl at the cluster the alert came from — the default
context is whichever one was configured last, and a wrong cluster fails with DNS errors that look
nothing like "wrong cluster":

```bash
export KUBECONFIG=/tmp/k3s.yaml            # k3s; make -C deploy k3s-kubeconfig to refresh
                                           # EKS: make -C deploy eks-kubeconfig
kubectl -n monitoring port-forward svc/kps-kube-prometheus-stack-prometheus 9090:9090 &
kubectl -n monitoring port-forward svc/kps-grafana 3001:80 &
```

PromQL below is written for the Prometheus UI. From `curl`, substitute a literal window for
`$__rate_interval` — it is a Grafana construct the API does not know.

---

## TicketBottleInternalErrors

**Means:** a service returned gRPC INTERNAL for five continuous minutes. The taxonomy reserves
INTERNAL for our own bugs, so this is a code fault rather than load or a dependency.
**Does not mean:** the event sold out (FAILED_PRECONDITION), or a dependency is down (UNAVAILABLE).

**First three checks**
1. `sum by (service, method) (tb:grpc_requests:rate5m{code="INTERNAL"})` — names the failing RPC.
   The alert itself aggregates with a bare `sum()` and carries no labels, so this is where the
   service comes from.
2. Dashboard A panel A4, stacked by `code` — confirms INTERNAL is its own band and not a
   misread of a FAILED_PRECONDITION spike next to it.
3. `kubectl -n ticketbottle logs deploy/<service> --since=10m | grep -i panic`.

**Resolved looks like:** the INTERNAL band returns to zero, about 5 minutes after the last error —
the `rate[5m]` window has to drain before the expression stops returning a series.
**If it does not resolve:** roll back that service's image tag.

---

## OutboxBacklogGrowing

**Means:** the payment outbox held more than 50 unpublished rows at every point in a 10-minute
window. The relay is not draining. Paid orders are not being confirmed, so buyers who have been
charged are still waiting for a ticket.
**Does not mean:** a burst of payments. The rule reads `min_over_time`, not the instant gauge,
precisely so a relay that claims a batch and publishes it — spiking the gauge for 200ms — does not
page. Exceeding 50 for ten solid minutes means the backlog never drained.

**First three checks**
1. `kubectl -n ticketbottle get pods -l app=outbox-relay` then
   `kubectl -n ticketbottle logs deploy/outbox-relay --tail=50` — is the relay running, and is it
   erroring on publish or sitting idle?
2. Dashboard C panel C3, both series — the instant gauge dipping while `min_over_time` stays high
   means it is draining but too slowly; neither dipping means it has stopped.
3. `histogram_quantile(0.95, sum by (le) (rate(tb_outbox_publish_lag_seconds_bucket[10m])))` —
   separates a stalled relay (lag climbing without bound) from one that is working but slower than
   the inflow.

**Resolved looks like:** `min_over_time(tb_outbox_pending_rows[10m])` drops under 50, which takes a
further 10 minutes after the backlog actually clears — the window has to refill with good samples.
Budget ~20 minutes end to end; this alert is slow by construction (10m window plus `for: 10m`).
**If it does not resolve:** check Redpanda is reachable from the relay pod. The relay claims rows
with `FOR UPDATE SKIP LOCKED`, so a transaction stuck open elsewhere holds rows invisible to it.

---

## SagaCompensationSpike

**Means:** CreateOrder workflows are rolling back at more than 0.5 compensation steps per second for
ten minutes. Something downstream of the reserve is failing consistently.
**Does not mean:** a failure count — `tb_order_compensations_total` increments once per compensation
*step*, so one failed saga rolling back two steps adds 2. It also does not mean the event sold out:
inventory is taken before anything is written, so a buyer who loses the race leaves nothing behind
and compensates nothing.

**First three checks**
1. `sum by (step) (rate(tb_order_compensations_total[5m]))` — dashboard B3. The `step` label names
   where the saga turned around.
2. `sum by (activity, code) (rate(tb_order_activity_failures_total[5m]))` — dashboard B4. The `code`
   is the taxonomy class of the hop that failed, and it decides what you do next.
3. `sum(rate(tb_order_compensations_total[5m])) / sum(rate(tb_order_workflow_duration_seconds_count{workflow="CreateOrder",outcome!="success"}[5m]))`
   — compensations per failed saga. A ratio near 1 is a late-stage failure; near 3 is failing at the
   last step and unwinding everything.

**Resolved looks like:** the rate returns to zero about 5 minutes after the last compensation.
**If it does not resolve:** route on the `code` from check 2. `UNAVAILABLE` is the named dependency
being down — go to its own pods. `FAILED_PRECONDITION` on a reserve compensation means holds are
expiring before payment completes; see `services/order-svc/docs/RESERVATION_HOLD.md`.

---

## OrdersNeedingRefund

**Means:** at least one order was written into REFUND_REQUIRED in the last ten minutes
(`tb_order_refund_required_total`, counted by `order-consumer` at the status write). A buyer was
charged and holds no ticket. Money is owed.
**Does not mean:** a sold-out buyer. Both arrive as FAILED_PRECONDITION from inventory, and the two
invert at the ledger:

```
   sold out          never charged, holds no ticket  → nothing owed  → silent by design
   REFUND_REQUIRED   WAS charged,   holds no ticket  → money owed    → this alert
```

**First three checks**
1. `sum(increase(tb_order_refund_required_total[1h]))` — how many buyers, not just whether it
   happened. One is a manual refund; twenty is an incident.
2. `kubectl -n ticketbottle logs deploy/order-consumer --since=1h | grep "cannot be fulfilled"` — the
   order codes, which is what you need to actually issue the refunds, and each one's `reason`.
3. Read the `reason`. `inventory could not be confirmed` means the hold expired and the stock was
   resold before the payment event arrived; `payment settled on an order in status …` means the
   payment landed after the order had already ended. Both are the hold window losing to payment
   latency — `services/order-svc/docs/RESERVATION_HOLD.md`.

**Resolved looks like:** nothing. The alert clears ten minutes after the last occurrence, but
`order.refund_required` has no consumer — no part of the system acts on that state. The alert going
quiet means it stopped happening, not that anyone was refunded.
**If it does not resolve:** issue the refunds by hand in the payment provider and leave the orders in
REFUND_REQUIRED. A consumer for that topic is the real fix.

---

## TargetDown

**Means:** a scrape target in the `ticketbottle` namespace has failed its scrapes for three minutes.
The target still exists in service discovery — Prometheus knows about it and cannot reach it.
**Does not mean:** the pod is gone. A deleted pod leaves service discovery and takes its `up` series
with it, so there is nothing left to compare against zero — that is MetricsMissing's job, not this
one.

**First three checks**
1. `up{namespace="ticketbottle"} == 0` — names the `job` and `pod`. Both labels are stamped by
   Prometheus from the target, not emitted by the service.
2. `kubectl -n ticketbottle get pods` — distinguishes CrashLoopBackOff (the app is down) from
   Running (the app is up and the metrics endpoint is not).
3. `kubectl -n ticketbottle port-forward <pod> 2112:2112` then `curl -s localhost:2112/metrics | head`
   — confirms whether port 2112 serves at all.

**Resolved looks like:** `up` returns to 1 within one scrape interval, 15s.
**If it does not resolve:** check the ServiceMonitor still matches the Service's labels and port
name. A renamed port silently produces a target that can never be scraped.

⚠️ While this fires, the Alertmanager inhibit rule suppresses every other `severity="page"` alert in
the same namespace. They are still in `/api/v2/alerts` with `status.state: "suppressed"` — check
there before concluding nothing else is wrong.

---

## MetricsMissing

**Means:** `up{job="waitroom-service"}` returns no series at all. The target has left service
discovery entirely — a deleted Deployment or Service, or a ServiceMonitor whose selector no longer
matches.
**Does not mean:** waitroom is down. The pod can be serving buyers perfectly while being invisible to
Prometheus. It also does not mean failing scrapes, which is TargetDown.

**First three checks**
1. `kubectl -n ticketbottle get svc waitroom-service` — does the Service still exist.
2. `kubectl -n ticketbottle get servicemonitor -o yaml | grep -B5 -A15 waitroom` — does its selector
   still match the Service's labels.
3. `count(up{namespace="ticketbottle"})` — expect 10. Tells you whether one target vanished or the
   whole namespace did.

**Resolved looks like:** the `up` series reappears within one scrape of discovery being fixed, and
`absent()` stops returning anything.
**If it does not resolve:** confirm `serviceMonitorSelectorNilUsesHelmValues: false` is still set in
`deploy/monitoring/values-kps.yaml`; without it the operator ignores ServiceMonitors that lack the
release label.

⚠️ **Scope.** This rule guards `waitroom-service` only, because `absent()` takes a single selector
and cannot be aggregated — widening it means one rule per job. The other nine are covered by
ScrapeTargetsMissing, which counts targets instead of naming one, at the cost of not telling you
which is missing.

---

## CheckoutBurnRateFast

**Means:** more than 14.4% of `POST /api/orders` took longer than 2 seconds, over both the last hour
and the last 5 minutes. That is 14.4× the 1% the SLO permits, and at that rate the 30-day error
budget is gone in about 2 days.
**Does not mean:** checkout is erroring. This SLI is latency only —
`tb_grpc_request_duration_seconds` carries no `code` label, so a request that failed with INTERNAL in
40ms counts as **good** because it was fast. Correctness is TicketBottleInternalErrors; read the two
together.

**First three checks**
1. `1 - tb:checkout_good:ratio5m` — the current burn rate against `1 - tb:checkout_good:ratio1h`.
   Short below long means it is already recovering and the alert is about to clear.
2. Dashboard A panel A3, p50 against p99 — p50 flat with p99 climbing is a subset of requests
   queueing behind something; both climbing is everything being slow.
3. Dashboard B panel B1, workflow duration by `outcome` — separates a slow saga from a slow gateway.
   If B1 is flat, the latency is in front of the workflow.

**Resolved looks like:** the alert clears within ~5 minutes of real recovery, because the short
window is the `and`'s reset leg — as soon as `ratio5m` drops back the expression stops returning a
series, even though `ratio1h` still carries the incident.
**If it does not resolve:** check A5 for the HPA. Desired pinned at max means the ceiling is the
constraint; raise `autoscaling.app-gateway.maxReplicas`.

---

## CheckoutBurnRateSlow

**Means:** more than 6% of checkouts exceeded 2 seconds across both 6 hours and 30 minutes. The
budget is being spent about six times faster than planned — exhausted in roughly 5 days. It is a
ticket, not a page.
**Does not mean:** an emergency, and not a duplicate of Fast. If both are firing, work Fast; Slow
firing alone is a degradation that has been present long enough to matter but is not acute.

**First three checks**
1. `1 - tb:checkout_good:ratio6h` against `1 - tb:checkout_good:ratio1h` — is it getting worse, or
   has it been sitting at this level?
2. Dashboard A3 over a 6h range — a step change points at a deploy; a slow ramp points at growth.
3. Dashboard A1 over the same range — if latency tracks request rate, this is capacity, not a bug.

**Resolved looks like:** clears when `ratio30m` recovers; the 6h leg lags well behind and is not what
releases it.
**If it does not resolve:** this is a capacity and tuning conversation, not an incident. Compare
against the bucket layout in `docs/METRICS.md` before moving the SLO.

---

## ScrapeTargetsMissing

**Means:** fewer than 10 targets in the `ticketbottle` namespace publish an `up` series at all. One
or more targets left service discovery — a terminated node, a Deployment scaled to zero, a deleted
Service, or a ServiceMonitor whose selector stopped matching.
**Does not mean:** a failing scrape. A discovered target that cannot be reached still writes `up = 0`
and belongs to TargetDown. This rule counts targets instead of comparing them to zero, which is why
it is the only one that sees a target that is simply gone.

**First three checks**
1. `count by (job) (up{namespace="ticketbottle"})` — the missing target is the `job` absent from the
   result, not a row reading zero.
2. `kubectl -n ticketbottle get pods -o wide` — a node's worth of pods gone points at the node; one
   Deployment's points at the workload.
3. `kubectl get nodes` — a `NotReady` node explains it, and `KubeNodeNotReady` should be firing
   alongside.

**Resolved looks like:** the count returns to 10 within one scrape of the pods rejoining Endpoints,
15s.
**If it does not resolve:** confirm 10 is still the right threshold. It is hardcoded in the rule, so
adding or retiring a workload makes this fire forever or go blind until the number is changed.
`kubectl -n ticketbottle get servicemonitor` is the other half of that check.

⚠️ **This one inhibits nothing.** The inhibit rule in `deploy/monitoring/values-kps.yaml` names
`TargetDown` as its only source, so during a node loss — the case this rule exists for — every other
`severity="page"` alert in the namespace fires alongside it instead of being suppressed. Expect a
burst, and work this one first.

---

## What is deliberately not here

There is no alert on `code="FAILED_PRECONDITION"`. Sold out, sale closed and wrong-state are business
outcomes, not faults, and paging on them turns a successful on-sale into an incident. The absence is
asserted, not merely intended: no rule in `prometheusrule.yaml` may reference the code. The one case
where a FAILED_PRECONDITION does page is OrdersNeedingRefund, and it pages on the ledger consequence
rather than on the code.
