package interceptors

import (
	"context"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func GrpcLoggingInterceptor(l logger.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		startTime := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(startTime)

		errCode := "OK"
		if err != nil {
			st, _ := status.FromError(err)
			errCode = st.Code().String()
		}

		ip := "unknown"
		if p, ok := peer.FromContext(ctx); ok {
			ip = p.Addr.String()
		}

		l.Infof(ctx, "%s %s - %dms - %s", info.FullMethod, errCode, duration.Milliseconds(), ip)

		return resp, err
	}
}
