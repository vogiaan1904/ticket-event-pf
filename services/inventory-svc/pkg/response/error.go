package response

import (
	pkgErrors "github.com/vogiaan/ticketbottle-inventory/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GrpcError(err error) error {
	switch parsedErr := err.(type) {
	case *pkgErrors.GRPCError:
		grpcCode := parsedErr.GrpcCode
		if grpcCode == 0 {
			grpcCode = codes.InvalidArgument
		}
		return status.Error(grpcCode, parsedErr.Error())
	default:
		return status.Error(codes.Internal, "Internal server error")
	}
}
