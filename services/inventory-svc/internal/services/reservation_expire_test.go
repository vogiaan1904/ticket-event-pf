package service

import (
	"context"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
)

func TestBatchExpire_ReleasesExpiredHolds(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	// Reserve in the past so the hold is already expired.
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-expire", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 7}},
	}))
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 7 {
		t.Fatalf("precondition reserved=%d, want 7", got.Reserved)
	}

	n, err := svc.BatchExpireReservations(context.Background(), 500)
	if err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired count = %d, want 1", n)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0 after expiry", got.Reserved)
	}

	var r models.Reservation
	repo.WithContext(context.Background()).Where("order_code = ?", "o-expire").First(&r)
	if r.Status != models.ReservationStatusExpired {
		t.Fatalf("status = %s, want EXPIRED", r.Status)
	}
}

// A reservation whose ticket class cannot absorb the decrement is corruption.
// Marking it EXPIRED anyway (the old behaviour) destroys the only evidence,
// so leave it ACTIVE and let the error log repeat every tick.
func TestBatchExpire_Drift_LeavesReservationActive(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0) // reserved = 0

	// Bypass Reserve so the reservation claims stock the counter never got.
	r := models.Reservation{
		OrderCode:     "o-drift",
		TicketClassID: tc.ID,
		Qty:           5,
		Status:        models.ReservationStatusActive,
		ExpiresAt:     time.Now().UTC().Add(-1 * time.Minute),
	}
	if err := repo.Create(context.Background(), &r); err != nil {
		t.Fatalf("seed reservation: %v", err)
	}

	n, err := svc.BatchExpireReservations(context.Background(), 500)
	if err != nil {
		t.Fatalf("BatchExpireReservations should skip drift, not fail the batch: %v", err)
	}
	if n != 0 {
		t.Fatalf("expired count = %d, want 0", n)
	}

	var got models.Reservation
	repo.WithContext(context.Background()).Where("order_code = ?", "o-drift").First(&got)
	if got.Status != models.ReservationStatusActive {
		t.Fatalf("status = %s, want ACTIVE (drift must stay visible)", got.Status)
	}
	if tcNow := ticketClassByID(t, repo, tc.ID); tcNow.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0 (unchanged)", tcNow.Reserved)
	}
}

// One poison ticket class must not block healthy reservations in the same
// batch -- otherwise the worker stalls forever behind a single bad row.
func TestBatchExpire_Drift_DoesNotBlockHealthyRows(t *testing.T) {
	svc, repo := reserveSvc(t)
	bad := seedTicketClass(t, repo, 100, 0, 0)
	good := seedTicketClass(t, repo, 100, 0, 0)

	poison := models.Reservation{
		OrderCode:     "o-drift-2",
		TicketClassID: bad.ID,
		Qty:           5,
		Status:        models.ReservationStatusActive,
		ExpiresAt:     time.Now().UTC().Add(-1 * time.Minute),
	}
	if err := repo.Create(context.Background(), &poison); err != nil {
		t.Fatalf("seed poison reservation: %v", err)
	}
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-healthy", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: good.ID, Qty: 3}},
	}))

	n, err := svc.BatchExpireReservations(context.Background(), 500)
	if err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired count = %d, want 1 (the healthy row only)", n)
	}
	if got := ticketClassByID(t, repo, good.ID); got.Reserved != 0 {
		t.Fatalf("healthy reserved = %d, want 0", got.Reserved)
	}
	if got := ticketClassByID(t, repo, bad.ID); got.Reserved != 0 {
		t.Fatalf("poison reserved = %d, want 0 (untouched)", got.Reserved)
	}
}
