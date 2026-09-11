package interceptors

import (
	"context"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func GrpcMetricsInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		metrics.GRPCInFlight.WithLabelValues(metrics.ServiceName).Inc()
		defer metrics.GRPCInFlight.WithLabelValues(metrics.ServiceName).Dec()

		startTime := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(startTime)

		errCode := "OK"
		if err != nil {
			st, _ := status.FromError(err)
			errCode = metrics.CodeString(st.Code())
		}

		metrics.GRPCRequests.WithLabelValues(metrics.ServiceName, info.FullMethod, errCode).Inc()
		metrics.GRPCDuration.WithLabelValues(metrics.ServiceName, info.FullMethod).Observe(duration.Seconds())

		return resp, err
	}
}
