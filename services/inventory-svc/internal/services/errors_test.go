package service

import (
	"context"
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
