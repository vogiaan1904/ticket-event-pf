# Instrumenting a workload

## The contract

Every workload publishes these three on `:2112/metrics`, plus whatever its domain needs:

| Metric | Type | Labels |
|---|---|---|
| `tb_grpc_requests_total` | counter | `service`, `method`, `code` |
| `tb_grpc_request_duration_seconds` | histogram | `service`, `method` |
| `tb_grpc_in_flight` | gauge | `service` |

`service` is the workload's own name and matches the Kubernetes Service fronting it. `code` is the gRPC code from the taxonomy in root `CLAUDE.md`.

**`method` does not mean the same thing everywhere.** The six gRPC services record `info.FullMethod` (`/order.OrderService/CreateOrder`); the gateway records its route (`POST /api/orders`). The counter stays summable across all seven because `code` is normalised; the histograms are not summable, because buckets differ.

## Go

Definitions in `internal/metrics/metrics.go`, recorded from a gRPC interceptor. Copy `services/order-svc/internal/metrics/metrics.go` — it is the reference implementation.

The non-obvious part is the `code` label. **`codes.Code.String()` returns the Go spelling (`"Internal"`), not the canonical wire spelling (`"INTERNAL"`)**, and the canonical table in gRPC-Go is unexported. The label is summed across Go and TS workloads and matched by name in alert rules, so the table is restated locally:

```go
func CodeString(c codes.Code) string {
	if name, ok := codeNames[c]; ok {
		return name
	}
	return fmt.Sprintf("CODE(%d)", c)
}
```

Getting this wrong does not error — it produces `code="Internal"` alongside `code="INTERNAL"` from the TS services, and `{code="INTERNAL"}` silently misses half the platform.

For errors that are not gRPC responses, classify them the same way. `RecordActivityFailure` uses `status.Code(err)`, which walks the `Unwrap` chain and therefore still sees a gRPC code through a `temporal.ApplicationError` wrapper.

## TypeScript

Definitions in `src/shared/metrics/metrics.ts` using `prom-client`, recorded from a NestJS interceptor (gRPC services) or middleware (the gateway). Copy `services/api-gateway/src/shared/metrics/metrics.ts`.

`collectDefaultMetrics()` is called deliberately — the Go services' `promhttp` handler serves the default registry, so their runtime metrics ship alongside the contract. This keeps the same bargain on both stacks.

## Choosing buckets

**A boundary is only worth a series where that workload's latency actually lands.** Do not copy another service's array.

| Workload | Boundaries | Sized for |
|---|---|---|
| `order-service` | `0.05 … 1, 2, 5, 10, 20, 30, 45, 60, 90, 120` | a saga bounded by `CreateOrderTimeout` (60s) |
| `app-gateway` | `0.05 … 1, 2, 3, 5, 10` | past 10s a gateway request is already an incident |
| `payment-service` | `0.01 … 1, 2.5, 5, 10` | a call leaving the cluster to a provider |
| `inventory`, `waitroom`, `event`, `user` | `0.005 … 1, 2.5, 5` | one locked Postgres transaction, or a Redis round-trip |

Two consequences, both binding:

- **`2` must stay a real boundary in the gateway's array.** The checkout SLO reads `le="2.0"` by name, and a bucket selector cannot interpolate between two boundaries the way `histogram_quantile` can. Removing it makes the burn-rate alerts permanently unfirable, silently.
- **Every query over this histogram names its workload.** See `docs/METRICS.md`.

## Cardinality

Series count is `workloads × methods × codes`, multiplied by every label you add. The rule that matters: **never label with an unbounded identifier** — no `order_id`, `user_id`, `payment_intent`.

`tb_waitroom_queue_depth` and `tb_waitroom_slots_in_use` do carry `event_id`, which is affordable only because `QueueDepth.Reset()` in `internal/metrics/sampler.go` rebuilds the set each tick. Series then track *events currently in play* rather than events ever seeded. If you add a bounded-in-practice label, make the mechanism that bounds it explicit like this, or it is unbounded.

`topk()` is a legibility fix, never a cardinality fix — Prometheus stores every series the target exposes regardless of what a query selects. The fix for cardinality is dropping the label at the source.

## Wiring a workload into scraping

Four things must line up. Any one missing produces no error anywhere:

1. **Serve `/metrics` on 2112.**
2. **A Service with a port named `metrics`.** ServiceMonitors select `endpoints[].port` by **name**, not number. Workloads that serve no cluster traffic (`order-consumer`, `outbox-relay`) get a headless Service from `templates/apps/metrics-service.yaml` purely so there is an object to select and Endpoints to discover.
3. **A `monitoring.targets.<name>: true` entry.** `values.yaml` keys all ten workloads to `false`; `values-k3s.yaml` turns them on. `servicemonitor.yaml` ranges over that map, so a workload absent from it is never scraped.
4. **The ServiceMonitor must be discoverable by the Prometheus CR.**

On point 4, this install sets `serviceMonitorSelectorNilUsesHelmValues: false` and `ruleSelectorNilUsesHelmValues: false` in `deploy/monitoring/values-kps.yaml`, which makes both selectors **empty — matching everything in every namespace**. So `monitoring.prometheusReleaseLabel` is `""` and the `release:` label is *not* load-bearing here.

⚠️ This is the opposite of the stock kube-prometheus-stack default, where a missing `release: <name>` label means the object applies cleanly, reports no error, and is never loaded. Check which regime you are in before debugging a rule or monitor that "isn't working":

```bash
kubectl -n monitoring get prometheus -o jsonpath='{.items[0].spec.ruleSelector}'
kubectl -n monitoring get prometheus -o jsonpath='{.items[0].spec.serviceMonitorSelector}'
# {}  → matches everything;  {"matchLabels":{"release":"kps"}} → the label is required
```

## Verifying new instrumentation

Metrics do not exist until traffic creates them, so assert **after** load, not before:

```bash
make -C deploy k3s-deploy
make -C deploy k3s-load &
curl -s localhost:9090/api/v1/query --data-urlencode \
  'query=count by (service) (tb_grpc_requests_total)'      # expect a row per workload
curl -s localhost:9090/api/v1/query --data-urlencode \
  'query=count by (code) (tb_grpc_requests_total)'         # codes must be UPPER_SNAKE
```

A `code` value in Go spelling (`Internal`) means `CodeString` was bypassed somewhere.
