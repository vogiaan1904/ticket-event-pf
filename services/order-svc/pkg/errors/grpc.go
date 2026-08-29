package errors

import (
	"fmt"

	"google.golang.org/grpc/codes"
)

type GRPCError struct {
	Message  string
	GrpcCode codes.Code
}

func NewGRPCError(code string, message string) *GRPCError {
	return &GRPCError{
		Message: fmt.Sprintf("%s - %s", code, message),
	}
}

// NewGRPCErrorWithStatus sets the status explicitly. NewGRPCError leaves
// GrpcCode zero, which response.GrpcError renders as InvalidArgument.
func NewGRPCErrorWithStatus(grpcCode codes.Code, code string, message string) *GRPCError {
	return &GRPCError{
		Message:  fmt.Sprintf("%s - %s", code, message),
		GrpcCode: grpcCode,
	}
}

func (e GRPCError) Error() string {
	return e.Message
}
