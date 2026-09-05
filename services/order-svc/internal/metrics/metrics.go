package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/status"
)

// durationBuckets extends past CreateOrderTimeout (60s) -- the default buckets
// top out at 10s, which would put every slow-but-legitimate order in +Inf and
// make 11s indistinguishable from 59s.
//
// The 2 boundary is the checkout SLO threshold (99% of POST /orders under 2s).
// histogram_quantile can interpolate, but an SLO ratio reads one bucket by
// name -- le="2" returns nothing unless 2 is a real boundary. Don't drop it.
var durationBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 45, 60, 90, 120}

var (
	GRPCRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tb_grpc_requests_total",
			Help: "Total gRPC requests handled, by service, method and result code.",
		},
		[]string{"service", "method", "code"},
	)

	GRPCDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tb_grpc_request_duration_seconds",
			Help:    "gRPC request duration in seconds, by service and method.",
			Buckets: durationBuckets,
		},
		[]string{"service", "method"},
	)

	GRPCInFlight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tb_grpc_in_flight",
			Help: "gRPC requests currently being handled, by service.",
		},
		[]string{"service"},
	)

	WorkflowDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tb_order_workflow_duration_seconds",
			Help:    "CreateOrder workflow duration in seconds, by workflow and outcome.",
			Buckets: durationBuckets,
		},
		[]string{"workflow", "outcome"},
	)

	Compensations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tb_order_compensations_total",
			Help: "Saga compensation steps executed, by step.",
		},
		[]string{"step"},
	)

	ActivityFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tb_order_activity_failures_total",
			Help: "Temporal activity failures, by activity and result code.",
		},
		[]string{"activity", "code"},
	)
)

// RecordActivityFailure classifies err the same way the gRPC interceptor
// classifies a response -- its gRPC code if it has one (status.Code walks
// the error's Unwrap chain, so this also sees through a
// temporal.ApplicationError wrapping a gRPC error) -- and increments
// ActivityFailures. A no-op on nil, so activities can call it unconditionally
// on their return path.
func RecordActivityFailure(activity string, err error) {
	if err == nil {
		return
	}
	ActivityFailures.WithLabelValues(activity, status.Code(err).String()).Inc()
}
