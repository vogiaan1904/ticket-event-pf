package response

import (
	pkgErrors "github.com/vogiaan1904/ticketbottle-order/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GrpcError(err error) error {
	switch parsedErr := err.(type) {
	case *pkgErrors.GRPCError:
		// No fallback for a zero GrpcCode: NewGRPCError requires one, and a
		// silent default is how an error ends up with the wrong class.
		return status.Error(parsedErr.GrpcCode, parsedErr.Error())
	default:
		return status.Error(codes.Internal, "Internal server error")
	}
}
