package service

import (
	"context"
	"errors"
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

// An ACTIVE hold past expires_at still holds its qty (unswept), so confirming
// is correct -- refusing left payments captured with no seat.
func TestConfirm_ActivePastExpiry_Succeeds(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-past-exp", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))

	if err := svc.Confirm(context.Background(), "o-past-exp"); err != nil {
		t.Fatalf("Confirm on an unswept past-expiry hold: %v", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != 0 || got.Sold != 5 {
		t.Fatalf("reserved=%d sold=%d, want 0/5", got.Reserved, got.Sold)
	}
}

// The worker already released the hold, but the seats are still there:
// re-acquire them rather than fail a paid order.
func TestConfirm_ExpiredByWorker_ReacquiresWhenStockAvailable(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-reacq", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))
	if _, err := svc.BatchExpireReservations(context.Background(), 500); err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("precondition reserved=%d, want 0 after the worker swept", got.Reserved)
	}

	if err := svc.Confirm(context.Background(), "o-reacq"); err != nil {
		t.Fatalf("Confirm on a swept hold with stock still free: %v", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != 0 || got.Sold != 5 {
		t.Fatalf("reserved=%d sold=%d, want 0/5 (stock re-acquired)", got.Reserved, got.Sold)
	}

	var r models.Reservation
	repo.WithContext(context.Background()).Where("order_code = ?", "o-reacq").First(&r)
	if r.Status != models.ReservationStatusConfirmed {
		t.Fatalf("status = %s, want CONFIRMED", r.Status)
	}
}

// The seats really are gone -- the caller must learn that and refund.
func TestConfirm_ExpiredByWorker_ConflictsWhenSoldOut(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 5, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-soldout", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))
	if _, err := svc.BatchExpireReservations(context.Background(), 500); err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}
	// Somebody else took every seat in the meantime.
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-winner", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))
	must(t, svc.Confirm(context.Background(), "o-winner"))

	if err := svc.Confirm(context.Background(), "o-soldout"); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Confirm on a swept hold with no stock left = %v, want ErrStateConflict", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Sold != 5 {
		t.Fatalf("sold = %d, want 5 (the failed re-acquire must not oversell)", got.Sold)
	}
}
