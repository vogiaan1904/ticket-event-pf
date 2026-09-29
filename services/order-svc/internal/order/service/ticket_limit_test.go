package service

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
)

func limitedCreateService(t *testing.T, limit int32) (*implService, *fakeTemporalClient, func() string) {
	t.Helper()
	tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{}}
	svc, r := newCreateService(t, tprCli, false, "")
	svc.evSvc = stubEventClient{maxTicketsPerOrder: limit}
	return svc, tprCli, func() string { return slotHolder(t, r, testUserEventSlotKey) }
}

// The limit counts tickets across every item, and refuses before the purchase
// slot is claimed or the saga starts, so a refused order holds nothing.
func TestCreate_AnOrderOverItsEventsLimitHoldsNothing(t *testing.T) {
	svc, tprCli, holder := limitedCreateService(t, 3)
	in := createInput()
	in.Items = []order.OrderItemInput{{TicketClassID: "tc1", Quantity: 2}, {TicketClassID: "tc2", Quantity: 2}}

	_, err := svc.Create(context.Background(), in)

	if !errors.Is(err, order.ErrTooManyTicketsInOrder) {
		t.Fatalf("err = %v, want ErrTooManyTicketsInOrder", err)
	}
	if tprCli.startedInput != nil {
		t.Fatal("the saga started for a refused order")
	}
	if h := holder(); h != "" {
		t.Fatalf("slot held by %s after a refused order", h)
	}
}

func TestCreate_AnOrderAtItsEventsLimitGoesAhead(t *testing.T) {
	svc, tprCli, _ := limitedCreateService(t, 2)

	if _, err := svc.Create(context.Background(), createInput()); err != nil {
		t.Fatalf("an order of 2 at a limit of 2: %v", err)
	}
	if tprCli.startedInput == nil {
		t.Fatal("the saga did not start")
	}
}

// 0 is what an event-svc that predates the field sends, so it must refuse nothing.
func TestCreate_AnEventWithNoLimitSetRefusesNothing(t *testing.T) {
	svc, tprCli, _ := limitedCreateService(t, 0)
	in := createInput()
	in.Items = []order.OrderItemInput{{TicketClassID: "tc1", Quantity: 50}}

	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatalf("an order of 50 with no limit set: %v", err)
	}
	if tprCli.startedInput == nil {
		t.Fatal("the saga did not start")
	}
}
