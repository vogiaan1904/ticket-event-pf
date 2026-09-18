# Metric contract

Every workload publishes the same three names from its `/metrics` endpoint on
port 2112, plus whatever is specific to its own domain.

| Metric | Type | Labels |
|---|---|---|
| `tb_grpc_requests_total` | counter | `service`, `method`, `code` |
| `tb_grpc_request_duration_seconds` | histogram | `service`, `method` |
| `tb_grpc_in_flight` | gauge | `service` |

`service` is the workload's own name and matches the Kubernetes Service that
fronts it. `code` is the gRPC code from the error taxonomy in `CLAUDE.md` — the
API Gateway serves HTTP but records the gRPC code it mapped from, so the label
means the same thing on all seven.

`method` does **not** mean the same thing on all seven. The six gRPC services
record `info.FullMethod` (`/order.OrderService/CreateOrder`); the gateway
records its route (`POST /api/orders`).

## Histogram buckets are tuned per workload

A boundary is only worth a series where that workload's latency actually lands,
so the layouts differ:

| Workload | Boundaries | Sized for |
|---|---|---|
| `order-service` | `0.05 … 1, 2, 5, 10, 20, 30, 45, 60, 90, 120` | a saga bounded by `CreateOrderTimeout` (60s) |
| `app-gateway` | `0.05 … 1, 2, 3, 5, 10` | past 10s a gateway request is already an incident |
| `payment-service` | `0.01 … 1, 2.5, 5, 10` | a call that leaves the cluster to a provider |
| `inventory`, `waitroom`, `event`, `user` | `0.005 … 1, 2.5, 5` | one locked Postgres transaction, or a Redis round-trip |

Above `le="1"` the only boundaries all seven share are `5` and `+Inf`.

### The rule this forces

> **Every query over `tb_grpc_request_duration_seconds` names its workload** —
> a `service` matcher, or `service` kept in the `by` list.

`sum by (le)` adds the series carrying each `le` value. Where a boundary exists
on only some workloads, the sum at that boundary omits the rest, and a
cumulative bucket that omits observations below it is not a cumulative bucket.
`histogram_quantile` does not error on that input: it clamps each bucket to the
running maximum and reads the quantile off a distribution no service observed,
leaving only a `PromQL info: … fixed for monotonicity` notice behind.

The rule is scoped to this one metric on purpose. Every other histogram
(`tb_order_workflow_duration_seconds`, `tb_outbox_publish_lag_seconds`) has a
single publisher, so a bare `sum by (le)` over it is already unambiguous.

## The checkout SLO

**99% of `POST /api/orders` complete in under 2 seconds.**

It is measured at the gateway, because that is the request the buyer makes. The
saga's own `CreateOrder` latency is a component of it, not a substitute: an SLO
read from `order-service` cannot see gateway queueing, auth, or the HTTP leg,
and would stay green through exactly the incident the gateway is the bottleneck
for.

```promql
sum(rate(tb_grpc_request_duration_seconds_bucket{service="app-gateway",method="POST /api/orders",le="2.0"}[1h]))
  / sum(rate(tb_grpc_request_duration_seconds_count{service="app-gateway",method="POST /api/orders"}[1h]))
```

`2` must therefore be a real boundary in the gateway's bucket array. A bucket
selector reads one boundary by name and cannot interpolate between two.

### `le` is a string, and it is not spelled the way you exposed it

The client libraries expose `le="2"`. Prometheus scrapes over OpenMetrics,
which requires bucket boundaries to be valid floats, so what is **stored** is
`le="2.0"` — for every workload, Go and TypeScript alike.

```promql
count(tb_grpc_request_duration_seconds_bucket{le="2"})     # 0   — matches nothing
count(tb_grpc_request_duration_seconds_bucket{le="2.0"})   # 7   — the real label
```

`le` is an ordinary string label and `"2" != "2.0"`, so the wrong spelling
returns an empty vector rather than an error. A recording rule built on it
records nothing, and a burn-rate alert reading that rule computes
`1 - <empty>` and can never fire. Read the boundary off
`count by (le) (...)` before writing any selector against it; do not copy the
number out of the exposition text.

## Labels under a cardinality condition

`tb_waitroom_queue_depth` and `tb_waitroom_slots_in_use` carry `event_id`. The
sampler `Reset()`s both on every tick, so the series count tracks events
currently in play rather than events ever seeded — that reset, not a seed-count
ceiling, is what bounds them.

`tb_waitroom_slots_in_use` is per event because the limit it is read against,
`QUEUE_DEFAULT_MAX_CONCURRENT`, is `MaxConcurrentPerEvent`. A cluster-wide sum
cannot be compared to a per-event cap in either direction: many events lightly
loaded exceeds it while none is saturated, and one event fully saturated does
not.
