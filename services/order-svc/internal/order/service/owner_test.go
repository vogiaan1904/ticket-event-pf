package service

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
)

func TestGetByID_AStrangerIsToldThereIsNoSuchOrder(t *testing.T) {
	svc, r := newSlotService(t)
	seedOrder(t, r, "TB-OWN-0001", models.OrderStatusPending)

	if _, err := svc.GetByID(context.Background(), "TB-OWN-0001", "u2"); !errors.Is(err, order.ErrOrderNotFound) {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
}

func TestGetByID_TheOwnerSeesTheItems(t *testing.T) {
	svc, r := newSlotService(t)
	seedOrder(t, r, "TB-OWN-0002", models.OrderStatusCompleted)
	if _, err := r.CreateManyItems(context.Background(), "TB-OWN-0002", []repo.CreateOrderItemOption{
		{TicketClassID: "tc1", TicketClassName: "General", Quantity: 2, PriceAtPurchase: 1000, TotalAmount: 2000},
	}); err != nil {
		t.Fatalf("seed items: %v", err)
	}

	out, err := svc.GetByID(context.Background(), "TB-OWN-0002", "u1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].TicketClassID != "tc1" {
		t.Fatalf("items = %+v, want the one tc1 line", out.Items)
	}
}

// Refused before any state check, so a stranger cannot learn the order's status.
func TestCancel_AStrangerIsToldThereIsNoSuchOrder(t *testing.T) {
	svc, r := newSlotService(t)
	seedOrder(t, r, "TB-OWN-0003", models.OrderStatusPending)

	if err := svc.Cancel(context.Background(), "TB-OWN-0003", "u2"); !errors.Is(err, order.ErrOrderNotFound) {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
	if o, _ := r.GetByCode(context.Background(), "TB-OWN-0003"); o.Status != models.OrderStatusPending {
		t.Fatalf("status = %s, want PENDING untouched", o.Status)
	}
}
