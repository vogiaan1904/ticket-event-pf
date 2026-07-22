package interceptors

import (
	"context"
	"runtime/debug"

	"github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GrpcRecoveryInterceptor turns a panic inside a handler into an Internal
// status error instead of letting it unwind into the runtime.
//
// grpc-go does not recover handler panics on its own: without this, a single
// nil dereference in one RPC kills the process and takes every in-flight
// reservation transaction with it. This must be the outermost interceptor so
// it also covers panics raised by the ones inside it.
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
