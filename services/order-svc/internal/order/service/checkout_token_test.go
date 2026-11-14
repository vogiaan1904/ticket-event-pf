package service

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The waitroom decides whether a token still buys; order-svc only asks.
func TestCreate_AsksTheWaitroomAboutTheTokenItWasGiven(t *testing.T) {
	tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{}}
	s, _ := newCreateService(t, tprCli, true, "sess-1")
	wr := s.wrSvc.(*stubWaitroomClient)

	if _, err := s.Create(context.Background(), createInput()); err != nil {
		t.Fatalf("create: %v", err)
	}
	in := createInput()
	if wr.asked == nil || wr.asked.Token != in.CheckoutToken || wr.asked.UserId != in.UserID || wr.asked.EventId != in.EventID {
		t.Fatalf("waitroom asked %+v, want token, user and event of the create", wr.asked)
	}
}

func TestCreate_ARefusedTokenTakesNoSlot(t *testing.T) {
	cases := []struct {
		name string
		code codes.Code
		want error
	}{
		{"refused by the waitroom", codes.Unauthenticated, order.ErrInvalidCheckoutToken},
		{"waitroom down", codes.Unavailable, order.ErrWaitroomUnavailable},
		{"waitroom too slow", codes.DeadlineExceeded, order.ErrWaitroomUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{}}
			s, r := newCreateService(t, tprCli, true, "sess-1")
			s.wrSvc.(*stubWaitroomClient).err = status.Error(tc.code, "no")

			_, err := s.Create(context.Background(), createInput())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tprCli.startedInput != nil {
				t.Fatalf("a refused token started a CreateOrder workflow")
			}
			if held := slotHolder(t, r, order.PurchaseSlotKey("sess-1", testUserID, testEventID)); held != "" {
				t.Fatalf("a refused token holds the slot: %q", held)
			}
		})
	}
}

// A code the waitroom never sends for a token is a fault, not a refusal.
func TestCreate_AnUnexpectedWaitroomCodeIsNotARefusal(t *testing.T) {
	tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{}}
	s, _ := newCreateService(t, tprCli, true, "sess-1")
	s.wrSvc.(*stubWaitroomClient).err = status.Error(codes.Internal, "boom")

	_, err := s.Create(context.Background(), createInput())
	if errors.Is(err, order.ErrInvalidCheckoutToken) || errors.Is(err, order.ErrWaitroomUnavailable) || status.Code(err) != codes.Internal {
		t.Fatalf("err = %v, want the waitroom's INTERNAL unchanged", err)
	}
}
