package service

import (
	"context"
	"testing"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
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

func TestRelease_GuardFailure_ErrorsAndRollsBack(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0) // reserved = 0

	// Corrupt state: an ACTIVE reservation whose qty exceeds the ticket
	// class's reserved counter (created directly, bypassing Reserve, so
	// reserved stays 0 while the reservation claims qty 5).
	r := models.Reservation{
		OrderCode:     "o-guard",
		TicketClassID: tc.ID,
		Qty:           5,
		Status:        models.ReservationStatusActive,
		ExpiresAt:     future(),
	}
	if err := repo.Create(context.Background(), &r); err != nil {
		t.Fatalf("seed reservation: %v", err)
	}

	if err := svc.Release(context.Background(), "o-guard"); err == nil {
		t.Fatal("expected Release to error when reserved < qty")
	}

	// Tx must have rolled back: reservation still ACTIVE, reserved still 0.
	var got models.Reservation
	repo.WithContext(context.Background()).Where("order_code = ?", "o-guard").First(&got)
	if got.Status != models.ReservationStatusActive {
		t.Fatalf("reservation status = %s, want ACTIVE (rolled back)", got.Status)
	}
	if tcNow := ticketClassByID(t, repo, tc.ID); tcNow.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0 (unchanged)", tcNow.Reserved)
	}
}
