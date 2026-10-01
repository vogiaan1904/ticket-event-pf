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

// An empty cursor is the first page; refusing it made the list unreadable.
func TestGetManyOrders_TheFirstPageNeedsNoCursor(t *testing.T) {
	s := &grpcService{}
	if err := s.validateGetManyOrdersRequest(&orderpb.GetManyOrdersRequest{PageSize: 10}); err != nil {
		t.Fatalf("first page refused: %v", err)
	}
}

func TestReadAndCancelRefuseARequestWithNoOwner(t *testing.T) {
	s := &grpcService{}
	if err := s.validateGetOrderRequest(&orderpb.GetOrderRequest{Code: "TB-1"}); err == nil {
		t.Error("a read with no user_id was accepted")
	}
	if err := s.validateCancelOrderRequest(&orderpb.CancelOrderRequest{Code: "TB-1"}); err == nil {
		t.Error("a cancel with no user_id was accepted")
	}
}

func TestCreateOrderInput_KeepsEveryFieldTheBuyerSent(t *testing.T) {
	in := newCreateOrderInput(&orderpb.CreateOrderRequest{
		UserId: "u1", EventId: "e1", UserFullname: "A", UserEmail: "a@x", UserPhone: "0900000000",
		PaymentMethod: "ZALOPAY", Currency: "VND", CheckoutToken: "tok", RedirectUrl: "https://r",
		Items: []*orderpb.CreateOrderItem{{TicketClassId: "tc1", Quantity: 2}},
	})
	if in.Phone != "0900000000" || in.Email != "a@x" || len(in.Items) != 1 || in.Items[0].Quantity != 2 {
		t.Fatalf("input = %+v", in)
	}
}

// price_cents is what one ticket cost; a client multiplies it by the quantity.
func TestOrderItemsCarryTheUnitPrice(t *testing.T) {
	itms := (&grpcService{}).newOrderItems([]models.OrderItem{{TicketClassID: "tc1", Quantity: 2, PriceAtPurchase: 12345, TotalAmount: 24690}})
	if len(itms) != 1 || itms[0].PriceCents != 12345 {
		t.Fatalf("items = %+v, want price_cents 12345 for one ticket", itms)
	}
}
