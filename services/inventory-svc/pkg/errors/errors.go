package errors

import (
	"fmt"

	"google.golang.org/grpc/codes"
)

type GRPCError struct {
	GrpcCode codes.Code
	Message  string
}

func (e *GRPCError) Error() string {
	return e.Message
}

func NewGRPCError(code codes.Code, message string) *GRPCError {
	return &GRPCError{
		GrpcCode: code,
		Message:  message,
	}
}

func NewGRPCErrorf(code codes.Code, format string, args ...interface{}) *GRPCError {
	return &GRPCError{
		GrpcCode: code,
		Message:  fmt.Sprintf(format, args...),
	}
}

var (
	ErrNotFound          = NewGRPCError(codes.NotFound, "resource not found")
	ErrInvalidArgument   = NewGRPCError(codes.InvalidArgument, "invalid argument")
	ErrAlreadyExists     = NewGRPCError(codes.AlreadyExists, "resource already exists")
	ErrInternal          = NewGRPCError(codes.Internal, "internal server error")
	ErrUnavailable       = NewGRPCError(codes.Unavailable, "service unavailable")
	ErrInsufficientStock = NewGRPCError(codes.ResourceExhausted, "insufficient stock")
)
