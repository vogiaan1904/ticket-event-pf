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

func (e GRPCError) Error() string {
	return e.Message
}
