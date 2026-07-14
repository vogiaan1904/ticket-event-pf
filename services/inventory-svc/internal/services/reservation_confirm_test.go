package service

import (
	"context"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
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

func TestConfirm_MixedState_ReturnsConflict(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc1 := seedTicketClass(t, repo, 100, 0, 0)
	tc2 := seedTicketClass(t, repo, 100, 0, 0)
	// One ACTIVE, one already CONFIRMED under the same order — an inconsistent
	// state Confirm must reject rather than partially re-apply.
	active := models.Reservation{OrderCode: "o-mixed", TicketClassID: tc1.ID, Qty: 1, Status: models.ReservationStatusActive, ExpiresAt: future()}
	confirmed := models.Reservation{OrderCode: "o-mixed", TicketClassID: tc2.ID, Qty: 1, Status: models.ReservationStatusConfirmed, ExpiresAt: future()}
	if err := repo.Create(context.Background(), &active); err != nil {
		t.Fatalf("seed active: %v", err)
	}
	if err := repo.Create(context.Background(), &confirmed); err != nil {
		t.Fatalf("seed confirmed: %v", err)
	}

	if err := svc.Confirm(context.Background(), "o-mixed"); err != ErrStateConflict {
		t.Fatalf("Confirm on mixed state = %v, want ErrStateConflict", err)
	}
}
