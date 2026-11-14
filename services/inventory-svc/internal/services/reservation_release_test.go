package service

import (
	"context"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
)

func TestRelease_FreesReserved(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 6}},
	}))

	if err := svc.Release(context.Background(), "o-rel", ReleaseUndone); err != nil {
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
	must(t, svc.Release(context.Background(), "o-rel2", ReleaseUndone))

	if err := svc.Release(context.Background(), "o-rel2", ReleaseUndone); err != nil {
		t.Fatalf("second Release should be a no-op, got: %v", err)
	}
}

func TestRelease_NoReservations_NoOp(t *testing.T) {
	svc, _ := reserveSvc(t)
	if err := svc.Release(context.Background(), "ghost", ReleaseUndone); err != nil {
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

	if err := svc.Release(context.Background(), "o-rel3", ReleaseUndone); err != ErrStateConflict {
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

	if err := svc.Release(context.Background(), "o-guard", ReleaseUndone); err == nil {
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

func reservationStatus(t *testing.T, repo *pkgGorm.Repository, oCode string) models.ReservationStatus {
	t.Helper()
	var r models.Reservation
	if err := repo.WithContext(context.Background()).Where("order_code = ?", oCode).First(&r).Error; err != nil {
		t.Fatalf("load reservation %s: %v", oCode, err)
	}
	return r.Status
}

func TestRelease_CheckoutEnded_MarksExpired(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel-ended", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 3}},
	}))

	must(t, svc.Release(context.Background(), "o-rel-ended", ReleaseCheckoutEnded))

	if st := reservationStatus(t, repo, "o-rel-ended"); st != models.ReservationStatusExpired {
		t.Fatalf("status = %s, want EXPIRED", st)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0", got.Reserved)
	}
}

func TestRelease_Undone_StillMarksCancelled(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel-undone", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 3}},
	}))

	must(t, svc.Release(context.Background(), "o-rel-undone", ReleaseUndone))

	if st := reservationStatus(t, repo, "o-rel-undone"); st != models.ReservationStatusCancelled {
		t.Fatalf("status = %s, want CANCELLED", st)
	}
}

// The worker swept the hold first; order-svc's release must not touch the counter again.
func TestRelease_CheckoutEnded_AfterSweep_IsNoOp(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel-swept", ExpiresAt: time.Now().UTC().Add(-time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 3}},
	}))
	if _, err := svc.BatchExpireReservations(context.Background(), 500); err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}

	must(t, svc.Release(context.Background(), "o-rel-swept", ReleaseCheckoutEnded))

	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0 (not decremented twice)", got.Reserved)
	}
	if st := reservationStatus(t, repo, "o-rel-swept"); st != models.ReservationStatusExpired {
		t.Fatalf("status = %s, want EXPIRED", st)
	}
}
