---
name: observability
description: Use when working on TicketBottle's metrics, dashboards or alerts — instrumenting a workload on :2112, writing or debugging PromQL, editing the PrometheusRule or the three Grafana dashboards, or running kube-prometheus-stack on the k3s box. Also for "why is this panel empty", "why did this query return NaN", "why didn't that alert fire", or any question about which gRPC code should page.
---

# Observability

Every workload serves a text document at `:2112/metrics`. Prometheus scrapes it every 15s into a local TSDB, and **everything downstream — dashboards, alert rules, the SLO burn rate, the runbooks — is a PromQL expression over that store.** Nothing pushes; a workload that nothing scrapes is not broken and raises no error.

```
   10 workloads :2112          Prometheus (kube-prometheus-stack, monitoring ns)
   ┌───────────────────┐       ┌──────────────────────────────────┐
   │ app-gateway       │  GET  │  TSDB  emptyDir, 6h retention    │
   │ 6 gRPC services   │◄──────┤        ↓                         │
   │ order-consumer    │  /15s │  rule engine ── recording rules  │
   │ outbox-relay      │       │             └─ alerting rules    │
   │ payment-webhook   │       └───────┬──────────────┬───────────┘
   └───────────────────┘               ▼              ▼
    ServiceMonitor selects        Grafana        Alertmanager
    the SERVICE, not the pod      3 dashboards   /api/v2/alerts
```

## Invariants

> **`INTERNAL` pages. `FAILED_PRECONDITION` never does.** Sold out, sale closed and wrong-state are business outcomes; paging on them turns a successful on-sale into an incident. The absence is asserted, not just intended — `helm template ... | grep 'expr:.*FAILED_PRECONDITION'` must return nothing. Grep the *expressions*, not the file: the rendered rule carries one deliberate comment naming the code, so a bare `grep -c` returns 1 and reads as a violation. The one exception pages on the *ledger*, not the code: `OrdersNeedingRefund` fires because the buyer was charged.

> **Every query over `tb_grpc_request_duration_seconds` names its workload** — a `service` matcher, or `service` in the `by` list. Buckets are tuned per workload, and `sum by (le)` across mismatched boundaries returns a plausible wrong number instead of an error. Full reasoning: `docs/METRICS.md`.

> **A `*Vec` publishes no series until a label combination is used.** After any restart, every counter and histogram in the contract is *absent* — not zero — until traffic arrives. An empty query result is the normal state of a freshly-rolled cluster, so never read "empty" as "broken" before checking whether traffic has happened.

> **Escape Prometheus templating inside chart templates.** `{{ $labels.job }}` in `prometheusrule.yaml` is evaluated by Helm first and fails the whole render with `undefined variable "$value"`. Write ``{{`{{ $labels.job }}`}}``.

> **The default kubectl context is not the k3s box.** It points at a torn-down EKS cluster and fails with DNS errors that look nothing like "wrong cluster". Every command here needs `export KUBECONFIG=/tmp/k3s.yaml` (refresh with `make -C deploy k3s-kubeconfig`).

## First response: a query returned nothing

Three results mean three different things, and only one is a defect:

| Result | Means | Do |
|---|---|---|
| `[]` empty | no series matched — never published, or a selector typo | run the ladder below |
| `NaN` | series exist; both legs of a ratio are 0 in this window | drive traffic, re-query |
| a number | working | — |

`count()` over a non-matching selector returns **empty, never 0** — so "expect 0" is unreachable advice for a missing metric.

**The ladder — relax one clause at a time, and note where series appear:**

```promql
count(tb_grpc_request_duration_seconds_bucket)                              # empty → nothing scraped, or no traffic since restart
count(...{service="app-gateway"})                                           # empty → that workload specifically
count(...{service="app-gateway",method="POST /api/orders"})                 # empty → that route has had no requests
count(...{service="app-gateway",method="POST /api/orders",le="2.0"})        # empty ONLY here → the bucket layout is wrong
```

Only the last step justifies editing instrumentation. Confirm scrape health first — `count(up{namespace="ticketbottle"})` should be 10, all `== 1`.

## Pick your reference

| You are… | Read |
|---|---|
| Debugging a query, an empty panel, or an alert that did not fire | [references/querying-and-debugging.md](references/querying-and-debugging.md) |
| Adding or changing metrics on a workload, or wiring a new one into scraping | [references/instrumenting-a-service.md](references/instrumenting-a-service.md) |
| Editing alert rules, dashboards, runbooks, or the kube-prometheus-stack values | [references/alerts-and-dashboards.md](references/alerts-and-dashboards.md) |

## File map

| Piece | Where |
|---|---|
| Metric contract — names, labels, bucket tables, the SLO | `docs/METRICS.md` |
| Go instrumentation | `services/{order,inventory,waitroom}-svc/internal/metrics/metrics.go` |
| TS instrumentation | `services/{api-gateway,user,event,payment}-svc/src/shared/metrics/metrics.ts` |
| Gateway HTTP middleware (records the gRPC code it mapped from) | `services/api-gateway/src/common/middlewares/metrics.middleware.ts` |
| Outbox relay metrics | `services/payment-svc/outbox-relay/src/metrics.ts` |
| ServiceMonitors (one per enabled target) | `deploy/helm/ticketbottle/templates/apps/servicemonitor.yaml` |
| Headless Services so non-serving workloads have something to select | `.../apps/metrics-service.yaml` |
| Recording + alerting rules | `.../apps/prometheusrule.yaml` |
| Per-workload scrape toggles | `values.yaml` → `monitoring.targets` (all `false`; `values-k3s.yaml` turns them on) |
| kube-prometheus-stack overlay | `deploy/monitoring/values-kps.yaml` |
| Dashboards (provisioned as labelled ConfigMaps) | `deploy/monitoring/dashboards/*.json` |
| Runbook, one section per alert | `docs/RUNBOOK.md` |

