package interceptors

import (
	"context"
	"time"

	"github.com/vogiaan1904/ticketbottle-order/internal/metrics"
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
		metrics.GRPCInFlight.WithLabelValues("order-service").Inc()
		defer metrics.GRPCInFlight.WithLabelValues("order-service").Dec()

		startTime := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(startTime)

		errCode := "OK"
		if err != nil {
			st, _ := status.FromError(err)
			errCode = st.Code().String()
		}

		metrics.GRPCRequests.WithLabelValues("order-service", info.FullMethod, errCode).Inc()
		metrics.GRPCDuration.WithLabelValues("order-service", info.FullMethod).Observe(duration.Seconds())

		return resp, err
	}
}
