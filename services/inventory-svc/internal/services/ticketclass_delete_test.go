package service

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
	"gorm.io/gorm"
)

func deleteSvc(t *testing.T) (TicketClassService, ReservationService, *pkgGorm.Repository) {
	t.Helper()
	repo := newTestDB(t)
	return NewTicketClassService(newTestLogger(), repo),
		NewReservationService(newTestLogger(), repo),
		repo
}

func reservationByOrderCode(t *testing.T, repo *pkgGorm.Repository, orderCode string) models.Reservation {
	t.Helper()
	var r models.Reservation
	if err := repo.WithContext(context.Background()).Where("order_code = ?", orderCode).First(&r).Error; err != nil {
		t.Fatalf("reload reservation for order_code=%s: %v", orderCode, err)
	}
	return r
}

func ticketClassExists(t *testing.T, repo *pkgGorm.Repository, id int64) bool {
	t.Helper()
	var count int64
	if err := repo.WithContext(context.Background()).Model(&models.TicketClass{}).
		Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatalf("count ticket class %d: %v", id, err)
	}
	return count > 0
}

// The P0: a CASCADE FK let DeleteTicketClass destroy every reservation,
// CONFIRMED ones included. A refused delete must leave both rows intact.
func TestDelete_ActiveReservation_ReturnsConflict(t *testing.T) {
	tcSvc, rSvc, repo := deleteSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, rSvc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-del-active", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 2}},
	}))

	if err := tcSvc.Delete(context.Background(), tc.ID); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Delete with an ACTIVE reservation = %v, want ErrStateConflict", err)
	}

	if !ticketClassExists(t, repo, tc.ID) {
		t.Fatalf("ticket class %d no longer exists after a refused delete", tc.ID)
	}
	r := reservationByOrderCode(t, repo, "o-del-active")
	if r.Status != models.ReservationStatusActive {
		t.Fatalf("reservation status = %s, want ACTIVE (must survive a refused delete)", r.Status)
	}
}

// The whole point of the fix: a CONFIRMED reservation is a paid order.
func TestDelete_ConfirmedReservation_ReturnsConflict(t *testing.T) {
	tcSvc, rSvc, repo := deleteSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, rSvc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-del-paid", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 2}},
	}))
	must(t, rSvc.Confirm(context.Background(), "o-del-paid"))

	if err := tcSvc.Delete(context.Background(), tc.ID); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Delete with a CONFIRMED (paid) reservation = %v, want ErrStateConflict", err)
	}

	if !ticketClassExists(t, repo, tc.ID) {
		t.Fatalf("ticket class %d no longer exists after a refused delete", tc.ID)
	}
	r := reservationByOrderCode(t, repo, "o-del-paid")
	if r.Status != models.ReservationStatusConfirmed {
		t.Fatalf("reservation status = %s, want CONFIRMED (the paid order must survive a refused delete)", r.Status)
	}
}

func TestDelete_OnlyTerminalReservations_Succeeds(t *testing.T) {
	tcSvc, rSvc, repo := deleteSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, rSvc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-del-cancelled", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 2}},
	}))
	must(t, rSvc.Release(context.Background(), "o-del-cancelled")) // -> CANCELLED

	if err := tcSvc.Delete(context.Background(), tc.ID); err != nil {
		t.Fatalf("Delete with only terminal reservations: %v", err)
	}
	if ticketClassExists(t, repo, tc.ID) {
		t.Fatalf("ticket class %d still exists after a permitted delete", tc.ID)
	}
}

func TestDelete_NoReservations_Succeeds(t *testing.T) {
	tcSvc, _, repo := deleteSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)

	if err := tcSvc.Delete(context.Background(), tc.ID); err != nil {
		t.Fatalf("Delete with no reservations: %v", err)
	}
	if ticketClassExists(t, repo, tc.ID) {
		t.Fatalf("ticket class %d still exists after delete", tc.ID)
	}
}

func TestDelete_UnknownID_NoOp(t *testing.T) {
	tcSvc, _, _ := deleteSvc(t)
	if err := tcSvc.Delete(context.Background(), 999999999); err != nil {
		t.Fatalf("Delete on unknown id = %v, want no-op success", err)
	}
}

// Calls the child-delete directly, bypassing the liveCount guard the way a
// future path that skips the ticket_class lock would: the statement itself
// must scope to terminal rows, not lean on the guard in front of it.
func TestDeleteTerminalReservations_LeavesLiveRowsUntouched(t *testing.T) {
	_, _, repo := deleteSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)

	seed := []models.Reservation{
		{OrderCode: "o-mix-active", TicketClassID: tc.ID, Qty: 1, Status: models.ReservationStatusActive, ExpiresAt: future()},
		{OrderCode: "o-mix-confirmed", TicketClassID: tc.ID, Qty: 1, Status: models.ReservationStatusConfirmed, ExpiresAt: future()},
		{OrderCode: "o-mix-expired", TicketClassID: tc.ID, Qty: 1, Status: models.ReservationStatusExpired, ExpiresAt: future()},
		{OrderCode: "o-mix-cancelled", TicketClassID: tc.ID, Qty: 1, Status: models.ReservationStatusCancelled, ExpiresAt: future()},
	}
	for i := range seed {
		must(t, repo.Create(context.Background(), &seed[i]))
	}

	if err := repo.GetDB().Transaction(func(tx *gorm.DB) error {
		return deleteTerminalReservations(tx, tc.ID)
	}); err != nil {
		t.Fatalf("deleteTerminalReservations: %v", err)
	}

	var remaining []models.Reservation
	if err := repo.WithContext(context.Background()).
		Where("ticket_class_id = ?", tc.ID).Find(&remaining).Error; err != nil {
		t.Fatalf("reload remaining reservations: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("remaining reservations = %d, want 2 (ACTIVE + CONFIRMED must survive)", len(remaining))
	}
	for _, r := range remaining {
		if r.Status != models.ReservationStatusActive && r.Status != models.ReservationStatusConfirmed {
			t.Fatalf("surviving reservation has status %s, want ACTIVE or CONFIRMED", r.Status)
		}
	}
}
