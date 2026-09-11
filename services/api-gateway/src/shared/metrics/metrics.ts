import { Counter, Gauge, Histogram, collectDefaultMetrics } from 'prom-client';

// The `service` label value: this workload's own name, matching the Kubernetes
// Service that fronts it.
export const SERVICE_NAME = 'app-gateway';

// le=2 is the checkout SLO threshold (99% of POST /orders under 2s) and the
// burn-rate rules read that bucket by name, so it must be a real boundary.
// Past 10s a gateway request is already an incident; the saga's 120s
// boundaries would only cost series here.
const durationBuckets = [0.05, 0.1, 0.25, 0.5, 1, 2, 3, 5, 10];

// The Go services' promhttp handler serves the default registry, so their
// runtime metrics ship with the contract's. This is the same bargain.
collectDefaultMetrics();

// Named for gRPC while serving HTTP: `method` holds a route and `code` holds
// the gRPC code the gateway mapped from, which is what keeps this summable
// with the six gRPC services. See docs/labs/aws-phaseD/05 §0.4.
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
