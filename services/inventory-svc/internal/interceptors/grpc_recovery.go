package interceptors

import (
	"context"
	"runtime/debug"

	"github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GrpcRecoveryInterceptor converts a handler panic into an Internal status.
// Why: grpc-go does not recover handler panics, so one nil dereference kills
// the process and every in-flight reservation transaction with it.
// Must be outermost, so it also covers panics from the interceptors inside it.
func GrpcRecoveryInterceptor(l logger.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				l.Errorf(ctx, "interceptors.GrpcRecovery: panic in %s: %v\n%s", info.FullMethod, r, debug.Stack())
				resp = nil
				err = status.Error(codes.Internal, "internal server error")
			}
		}()

		return handler(ctx, req)
	}
}
