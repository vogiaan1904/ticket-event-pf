package grpc

import (
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	orderpb "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/order"
)

// The order contract has no expired status; a buyer must see a finished checkout.
func TestATimedOutOrderReadsCanceled(t *testing.T) {
	if got := GrpcOrderStatusValue[models.OrderStatusTimeout]; got != orderpb.OrderStatus_ORDER_STATUS_CANCELED {
		t.Fatalf("TIMEOUT reads %s, want ORDER_STATUS_CANCELED", got)
	}
}
