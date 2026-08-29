package errors

import (
	"fmt"

	"google.golang.org/grpc/codes"
)

type GRPCError struct {
	Message  string
	GrpcCode codes.Code
}

// NewGRPCError requires the status class. It is the first argument so that
// omitting it cannot compile: every error must state whether it is a client
// mistake, a business rejection, or our bug. See the error taxonomy in the
// root CLAUDE.md.
func NewGRPCError(grpcCode codes.Code, code string, message string) *GRPCError {
	return &GRPCError{
		Message:  fmt.Sprintf("%s - %s", code, message),
		GrpcCode: grpcCode,
	}
}

func (e GRPCError) Error() string {
	return e.Message
}
