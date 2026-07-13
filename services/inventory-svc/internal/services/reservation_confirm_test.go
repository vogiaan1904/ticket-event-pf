package service

import (
	"context"
	"testing"
	"time"
)

func TestConfirm_MovesReservedToSold(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-confirm", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))

	if err := svc.Confirm(context.Background(), "o-confirm"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != 0 || got.Sold != 5 {
		t.Fatalf("reserved=%d sold=%d, want 0/5", got.Reserved, got.Sold)
	}
}

func TestConfirm_Idempotent_SecondCallNoOp(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-conf2", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))
	must(t, svc.Confirm(context.Background(), "o-conf2"))

	if err := svc.Confirm(context.Background(), "o-conf2"); err != nil {
		t.Fatalf("second Confirm should be a no-op, got: %v", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != 0 || got.Sold != 5 {
		t.Fatalf("reserved=%d sold=%d, want 0/5 (not double-applied)", got.Reserved, got.Sold)
	}
}

func TestConfirm_Expired_ReturnsConflict(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	// Reserve already-expired.
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-exp", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))

	err := svc.Confirm(context.Background(), "o-exp")
	if err != ErrStateConflict {
		t.Fatalf("Confirm on expired hold = %v, want ErrStateConflict", err)
	}
}

func TestConfirm_NoReservations_ReturnsNotFound(t *testing.T) {
	svc, _ := reserveSvc(t)
	if err := svc.Confirm(context.Background(), "nope"); err == nil {
		t.Fatal("expected an error confirming an unknown order")
	}
}
