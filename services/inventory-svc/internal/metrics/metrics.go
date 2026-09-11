package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/codes"
)

// ServiceName is the `service` label value: this workload's own name, matching
// the Kubernetes Service that fronts it.
const ServiceName = "inventory-service"

// durationBuckets are sized for a single locked Postgres transaction, not for a
// saga. A reserve past 5s means the row lock is contended, which the top bucket
// is there to show; order-svc's 120s boundaries would only cost series here.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

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

	Reserves = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tb_inventory_reserve_total",
			Help: "Reserve attempts, by outcome.",
		},
		[]string{"result"},
	)
)

// Reserve outcomes. sold_out is a buyer losing a race, not a fault: it leaves
// as FAILED_PRECONDITION and must never page.
const (
	ReserveReserved = "reserved"
	ReserveSoldOut  = "sold_out"
	ReserveError    = "error"
)

// codeNames is gRPC's canonical code spelling. codes.Code.String() returns the
// Go spelling ("Internal"), and the canonical form gRPC-Go uses on the wire is
// unexported, so the table is restated here.
// Why: `code` is summed across Go and TS services, and @grpc/grpc-js, the error
// taxonomy in the root CLAUDE.md and the alert rules all spell it this way.
var codeNames = map[codes.Code]string{
	codes.OK:                 "OK",
	codes.Canceled:           "CANCELLED",
	codes.Unknown:            "UNKNOWN",
	codes.InvalidArgument:    "INVALID_ARGUMENT",
	codes.DeadlineExceeded:   "DEADLINE_EXCEEDED",
	codes.NotFound:           "NOT_FOUND",
	codes.AlreadyExists:      "ALREADY_EXISTS",
	codes.PermissionDenied:   "PERMISSION_DENIED",
	codes.ResourceExhausted:  "RESOURCE_EXHAUSTED",
	codes.FailedPrecondition: "FAILED_PRECONDITION",
	codes.Aborted:            "ABORTED",
	codes.OutOfRange:         "OUT_OF_RANGE",
	codes.Unimplemented:      "UNIMPLEMENTED",
	codes.Internal:           "INTERNAL",
	codes.Unavailable:        "UNAVAILABLE",
	codes.DataLoss:           "DATA_LOSS",
	codes.Unauthenticated:    "UNAUTHENTICATED",
}

// CodeString returns the canonical name of c for the `code` label.
func CodeString(c codes.Code) string {
	if name, ok := codeNames[c]; ok {
		return name
	}
	return fmt.Sprintf("CODE(%d)", c)
}
