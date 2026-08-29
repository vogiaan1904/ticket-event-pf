package response

import (
	pkgErrors "github.com/vogiaan1904/ticketbottle-order/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// No fallback for a zero GrpcCode: it used to become InvalidArgument, which
// silently gave 24 of 25 errors the wrong class. NewGRPCError now requires one.
func GrpcError(err error) error {
	switch parsedErr := err.(type) {
	case *pkgErrors.GRPCError:
		return status.Error(parsedErr.GrpcCode, parsedErr.Error())
	default:
		return status.Error(codes.Internal, "Internal server error")
	}
}
