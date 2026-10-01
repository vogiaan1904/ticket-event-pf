package grpc

import (
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	orderpb "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/order"
)

var storedStatuses = []models.OrderStatus{
	models.OrderStatusPending,
	models.OrderStatusTimeout,
	models.OrderStatusCompleted,
	models.OrderStatusCancelled,
	models.OrderStatusPaymentFailed,
	models.OrderStatusRefunded,
	models.OrderStatusRefundRequired,
}

// A buyer owed money must not read the same thing as one who was never charged.
func TestEveryStoredStatusHasItsOwnWireValue(t *testing.T) {
	seen := map[orderpb.OrderStatus]models.OrderStatus{}
	for _, st := range storedStatuses {
		wire := GrpcOrderStatusValue[st]
		if wire == orderpb.OrderStatus_ORDER_STATUS_UNSPECIFIED {
			t.Errorf("%s reads UNSPECIFIED", st)
		}
		if prev, dup := seen[wire]; dup {
			t.Errorf("%s and %s both read %s", prev, st, wire)
		}
		seen[wire] = st
		if back := OrderStatus[wire]; back != st {
			t.Errorf("a filter on %s finds %q, want %s", wire, back, st)
		}
	}
}
