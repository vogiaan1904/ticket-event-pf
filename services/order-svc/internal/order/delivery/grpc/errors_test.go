package grpc

import (
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	pkgErrors "github.com/vogiaan1904/ticketbottle-order/pkg/errors"
	"google.golang.org/grpc/codes"
)

// Asking for too many tickets is the buyer's to fix, so it must not reach the
// gateway as INTERNAL, which pages.
func TestMapError_TooManyTicketsIsInvalidArgument(t *testing.T) {
	got, ok := (&grpcService{}).mapError(order.ErrTooManyTicketsInOrder).(*pkgErrors.GRPCError)
	if !ok || got.GrpcCode != codes.InvalidArgument {
		t.Fatalf("mapError = %v, want an InvalidArgument GRPCError", got)
	}
}
