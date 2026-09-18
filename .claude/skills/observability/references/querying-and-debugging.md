# Querying and debugging

## The three results, and what each rules out

A PromQL query that "doesn't work" returns one of three things. They have different causes and different fixes, and conflating them is the most common way to spend an hour on the wrong problem.

### `[]` — no series matched

Nothing in the TSDB satisfies the selector. Four causes, cheapest first:

1. **No traffic since the last restart.** The default state, not a fault. See *`*Vec` publication* below.
2. **A label value that never existed** — `method="POST /api/orders"` when the route is spelled differently, or `le="2"` when the stored value is `"2.0"`.
3. **The target is not scraped** — check `count(up{namespace="ticketbottle"})`, expect 10.
4. **The metric genuinely is not published.**

Isolate with the ladder in SKILL.md: relax one clause at a time and note *where* series first appear. The clause you removed when they appeared is the wrong one.

### `NaN` — the series exist, the window is empty

Almost always `0/0` from a ratio. Both legs evaluated to zero because no requests landed in the window:

```promql
tb:checkout_good:ratio5m                                     # NaN
sum(rate(..._bucket{...,le="2.0"}[5m]))                      # 0
sum(rate(..._count{...}[5m]))                                # 0
```

This is correct behaviour — an error ratio or a good-request ratio is *undefined* when nothing was requested. A blank error-ratio panel means "no traffic", not "no errors", which is why the request-rate panel belongs next to it.

**NaN never fires an alert.** PromQL comparisons against NaN are always false, so `(1 - NaN) > 0.144` filters the series out entirely. An idle cluster stays quiet for this reason, not by accident.

### A number

Working. For a ratio, expect `0`–`1`.

## `*Vec` publication: absent, not zero

A `CounterVec` / `HistogramVec` / `GaugeVec` registers **no series at all** until some code path calls `WithLabelValues(...)`. Consequences that look like bugs:

- After every rollout, `tb_grpc_requests_total` and `tb_grpc_request_duration_seconds` are absent platform-wide until the first request.
- `tb_order_compensations_total` and `tb_order_activity_failures_total` publish nothing on a healthy cluster — no saga has rolled back. Their dashboard panels are legitimately empty.
- An unlabelled gauge (`tb_outbox_pending_rows`) *does* publish at startup. Comparing a labelled metric against an unlabelled one is a fast way to tell "nothing scraped" from "nothing happened": if the gauge has samples and the counters don't, scraping is fine and traffic is the gap.

## A flat counter has a rate of zero forever

Because the series is created *already at* its first value, a counter observed exactly once never shows an increase:

```
samples:  (absent) … 1 1 1 1 1      rate() = 0 at every window width, including 6h
```

So one request is not enough to populate a rate-based panel or recording rule. `make -C deploy k3s-gate2` drives a single purchase — useful for creating the series, useless for producing a rate. Use `make -C deploy k3s-load` and query **while it runs**; a 5m window drains five minutes after load stops.

## `count()` returns empty, never 0

There is no series to count, so nothing is emitted. Any triage step phrased as "if this returns 0…" is unreachable for a missing metric. Test existence with `count(...)` and read *empty* as the signal.

## Recording rules inherit all of this

A recording rule whose expression evaluates to an empty vector **writes no series**. So `tb:checkout_good:ratio5m` returning `[]` is ambiguous between "the rule isn't deployed" and "the rule is deployed and its inputs are empty". Disambiguate against the live object, not the query:

```bash
kubectl -n ticketbottle get prometheusrule ticketbottle -o json \
  | jq -r '.spec.groups[].rules[] | (.record // .alert)'
```

Expect 5 recording + 8 alerting. Fewer means the chart was not redeployed after editing the template.

## Windows: `rate`, `irate`, `$__rate_interval`

Grafana sends `start`, `end` and a **`step`** computed from the time range and panel width. The range selector inside `rate()` is independent of the step, and that independence is where panels break.

| Mismatch | Symptom |
|---|---|
| window < step | steps never examined — the panel samples instants, not a rate |
| window < 4 × scrape interval | one missed scrape leaves a single sample; `rate()` returns nothing → dotted line |

`$__rate_interval` = `max(4 × scrape_interval, step + scrape_interval)`, which fixes both. Use it in **every dashboard panel**.

- **`irate` is for zooming in**, never for panels or alerts — it reads only the last two samples in the window, so at a 72s step it draws one 15s slice out of every 72 and discards the rest.
- **`$__rate_interval` does not work in alert rules.** There is no panel width and no time picker, so there is no step to derive from. Alert rules take a literal window (`[5m]`).
- **Substitute a literal window when testing from `curl`.** The Prometheus API does not know the variable and returns empty.

## Bucket selectors

`le` is an ordinary **string** label, and Prometheus stores the OpenMetrics float spelling:

```promql
count(tb_grpc_request_duration_seconds_bucket{le="2"})     # 0  — matches nothing
count(tb_grpc_request_duration_seconds_bucket{le="2.0"})   # 7  — the real label
```

`"2" != "2.0"`, and the wrong spelling returns an empty vector rather than an error — so a recording rule built on it records nothing and the burn-rate alert reading that rule can never fire. Read the boundary off `count by (le) (...)` before writing any selector; never copy the number out of the exposition text.

Buckets are tuned per workload, so **name the workload in every query over `tb_grpc_request_duration_seconds`**. `sum by (le)` across mismatched boundaries builds a non-cumulative curve that `histogram_quantile` silently clamps. Tables in `docs/METRICS.md`.

## Scrape health, in order

```bash
export KUBECONFIG=/tmp/k3s.yaml
kubectl -n monitoring port-forward svc/kps-kube-prometheus-stack-prometheus 9090:9090 &

curl -s localhost:9090/api/v1/query --data-urlencode 'query=count(up{namespace="ticketbottle"})'
curl -s localhost:9090/api/v1/query --data-urlencode 'query=up{namespace="ticketbottle"} == 0'
curl -s localhost:9090/api/v1/label/__name__/values | jq -r '.data[]' | grep ^tb_
```

⚠️ `__name__` lists any metric seen within retention, including ones with no current samples. A name appearing there is not proof the series exists now — confirm with `count()`.

## `job` and `namespace` on `up`

Neither label is emitted by the service; Prometheus synthesises `up` after each scrape and stamps it from the target:

```
namespace  ←  __meta_kubernetes_namespace     the Pod's namespace
job        ←  __meta_kubernetes_service_name  the Service's name
```

So `job` takes Service names — `app-gateway`, `order-service`, `outbox-relay-metrics`, … A selector like `job=~"ticketbottle.*"` matches **none** of them; no Service is named after the platform. Use `up{namespace="ticketbottle"}`.

## Default alerts that fire on k3s and are not yours

k3s runs the control plane inside a single server binary, so kube-prometheus-stack's built-in component rules find no targets and fire permanently:

```
KubeControllerManagerDown    KubeProxyDown    KubeSchedulerDown
```

`Watchdog` also fires always, by design — a heartbeat proving the rule engine reaches Alertmanager, since a system that never sends anything looks identical to a broken one. Expect four standing alerts on an idle healthy box, and filter to your own group when asserting "nothing is firing".
