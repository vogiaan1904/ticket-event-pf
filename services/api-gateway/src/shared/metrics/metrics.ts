import { Counter, Gauge, Histogram, collectDefaultMetrics } from 'prom-client';

// The `service` label value: this workload's own name, matching the Kubernetes
// Service that fronts it.
export const SERVICE_NAME = 'app-gateway';

// le=2 is the checkout SLO boundary: the burn-rate rules read
// tb_grpc_request_duration_seconds_bucket{service="app-gateway",
// method="POST /api/orders",le="2.0"} by name, so 2 must be a real boundary.
// Past 10s a gateway request is already an incident; the saga's 120s
// boundaries would only cost series here.
const durationBuckets = [0.05, 0.1, 0.25, 0.5, 1, 2, 3, 5, 10];

// The Go services' promhttp handler serves the default registry, so their
// runtime metrics ship with the contract's. This is the same bargain.
collectDefaultMetrics();

// Named for gRPC while serving HTTP: `method` holds a route and `code` holds
// the gRPC code the gateway mapped from, which is what keeps the counter
// summable with the six gRPC services. The histograms are not: see
// docs/METRICS.md.
export const grpcRequests = new Counter({
  name: 'tb_grpc_requests_total',
  help: 'Total requests handled, by service, method and result code.',
  labelNames: ['service', 'method', 'code'] as const,
});

export const grpcDuration = new Histogram({
  name: 'tb_grpc_request_duration_seconds',
  help: 'Request duration in seconds, by service and method.',
  labelNames: ['service', 'method'] as const,
  buckets: durationBuckets,
});

export const grpcInFlight = new Gauge({
  name: 'tb_grpc_in_flight',
  help: 'Requests currently being handled, by service.',
  labelNames: ['service'] as const,
});
