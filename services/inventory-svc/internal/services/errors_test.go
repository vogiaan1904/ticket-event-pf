package service

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
)

// The CHECK constraints are the backstop for every quantity bug in this
// service: if application logic ever lets reserved + sold exceed total, the
// database must refuse the write rather than oversell the event.
func TestCapacityConstraint_RejectsOversell(t *testing.T) {
	repo := newTestDB(t)
	tc := seedTicketClass(t, repo, 10, 0, 0)

	err := repo.WithContext(context.Background()).
		Model(&models.TicketClass{}).
		Where("id = ?", tc.ID).
		Update("reserved", 11).Error
	if err == nil {
		t.Fatal("expected the capacity CHECK constraint to reject reserved=11 on total=10")
	}
}

func TestCapacityConstraint_RejectsNegativeReserved(t *testing.T) {
	repo := newTestDB(t)
	tc := seedTicketClass(t, repo, 10, 0, 0)

	err := repo.WithContext(context.Background()).
		Model(&models.TicketClass{}).
		Where("id = ?", tc.ID).
		Update("reserved", -1).Error
	if err == nil {
		t.Fatal("expected the capacity CHECK constraint to reject reserved=-1")
	}
}

func TestReservationQtyConstraint_RejectsZero(t *testing.T) {
	repo := newTestDB(t)
	tc := seedTicketClass(t, repo, 10, 0, 0)

	r := models.Reservation{
		OrderCode:     "o-qty-zero",
		TicketClassID: tc.ID,
		Qty:           0,
		Status:        models.ReservationStatusActive,
		ExpiresAt:     future(),
	}
	if err := repo.Create(context.Background(), &r); err == nil {
		t.Fatal("expected the qty CHECK constraint to reject qty=0")
	}
}

func TestReserve_InsufficientStock_ReturnsDomainError(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 2, 0, 0)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-domain-stock", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	})
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("Reserve over capacity = %v, want ErrInsufficientStock", err)
	}
}

func TestReserve_UnknownTicketClass_ReturnsDomainError(t *testing.T) {
	svc, _ := reserveSvc(t)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-domain-missing", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: 999999999, Qty: 1}},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reserve on unknown ticket class = %v, want ErrNotFound", err)
	}
}

func TestConfirm_UnknownOrder_ReturnsDomainError(t *testing.T) {
	svc, _ := reserveSvc(t)

	if err := svc.Confirm(context.Background(), "o-domain-ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Confirm on unknown order = %v, want ErrNotFound", err)
	}
}

func TestGetByID_Unknown_ReturnsDomainError(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)

	_, err := tcSvc.GetByID(context.Background(), 999999999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByID on unknown id = %v, want ErrNotFound", err)
	}
}
