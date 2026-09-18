import { Counter, Gauge, Histogram, collectDefaultMetrics } from 'prom-client';

// The `service` label value: this workload's own name, matching the Kubernetes
// Service that fronts it.
export const SERVICE_NAME = 'payment-service';

// Sized for a call that leaves the cluster: CreateIntent waits on PayOS or
// ZaloPay, so the upper boundaries are provider latency, not database latency.
const durationBuckets = [0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10];

// The Go services' promhttp handler serves the default registry, so their
// runtime metrics ship with the contract's. This is the same bargain.
collectDefaultMetrics();

export const grpcRequests = new Counter({
  name: 'tb_grpc_requests_total',
  help: 'Total gRPC requests handled, by service, method and result code.',
  labelNames: ['service', 'method', 'code'] as const,
});

export const grpcDuration = new Histogram({
  name: 'tb_grpc_request_duration_seconds',
  help: 'gRPC request duration in seconds, by service and method.',
  labelNames: ['service', 'method'] as const,
  buckets: durationBuckets,
});

export const grpcInFlight = new Gauge({
  name: 'tb_grpc_in_flight',
  help: 'gRPC requests currently being handled, by service.',
  labelNames: ['service'] as const,
});
