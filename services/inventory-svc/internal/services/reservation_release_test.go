package service

import (
	"context"
	"testing"
)

func TestRelease_FreesReserved(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 6}},
	}))

	if err := svc.Release(context.Background(), "o-rel"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0", got.Reserved)
	}
}

func TestRelease_Idempotent_SecondCallNoOp(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel2", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 6}},
	}))
	must(t, svc.Release(context.Background(), "o-rel2"))

	if err := svc.Release(context.Background(), "o-rel2"); err != nil {
		t.Fatalf("second Release should be a no-op, got: %v", err)
	}
}

func TestRelease_NoReservations_NoOp(t *testing.T) {
	svc, _ := reserveSvc(t)
	if err := svc.Release(context.Background(), "ghost"); err != nil {
		t.Fatalf("Release on unknown order should be a no-op, got: %v", err)
	}
}

func TestRelease_Confirmed_ReturnsConflict(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel3", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 6}},
	}))
	must(t, svc.Confirm(context.Background(), "o-rel3"))

	if err := svc.Release(context.Background(), "o-rel3"); err != ErrStateConflict {
		t.Fatalf("Release on confirmed order = %v, want ErrStateConflict", err)
	}
}
