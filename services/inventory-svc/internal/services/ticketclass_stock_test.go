package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
)

// seedClassFor adds a ticket class to eventID, adjusted by opt.
func seedClassFor(t *testing.T, repo *pkgGorm.Repository, eventID string, total, reserved, sold int, opt func(*models.TicketClass)) {
	t.Helper()
	tc := models.TicketClass{
		EventID:    eventID,
		Name:       fmt.Sprintf("C-%d", seedCounter.Add(1)),
		PriceCents: 1000,
		Currency:   "USD",
		Total:      total,
		Reserved:   reserved,
		Sold:       sold,
		Status:     models.TicketClassStatusActive,
	}
	if opt != nil {
		opt(&tc)
	}
	if err := repo.Create(context.Background(), &tc); err != nil {
		t.Fatalf("seed ticket class: %v", err)
	}
}

// A class that can no longer sell must not hold a line open; one not on sale yet
// still counts toward what is left, but not toward what is available now.
func TestGetEventStock_CountsOnlyClassesThatCanStillSell(t *testing.T) {
	repo := newTestDB(t)
	svc := NewTicketClassService(newTestLogger(), repo)
	eID := "evt-" + t.Name()
	past, later := time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour)

	seedClassFor(t, repo, eID, 10, 3, 2, nil) // on sale: 5 available
	seedClassFor(t, repo, eID, 5, 0, 0, func(tc *models.TicketClass) { tc.SaleStartAt = &later })
	seedClassFor(t, repo, eID, 7, 0, 1, func(tc *models.TicketClass) { tc.SaleEndAt = &past })
	seedClassFor(t, repo, eID, 4, 0, 0, func(tc *models.TicketClass) { tc.Status = models.TicketClassStatusInactive })
	seedClassFor(t, repo, "evt-other-"+t.Name(), 50, 0, 0, nil)

	got, err := svc.GetEventStock(context.Background(), eID)
	must(t, err)

	if want := (EventStock{Total: 15, Sold: 2, Available: 5}); got != want {
		t.Fatalf("stock = %+v, want %+v", got, want)
	}
}

func TestGetEventStock_AnEventWithNoClassesHasNothing(t *testing.T) {
	repo := newTestDB(t)
	svc := NewTicketClassService(newTestLogger(), repo)

	got, err := svc.GetEventStock(context.Background(), "evt-none")
	must(t, err)

	if got != (EventStock{}) {
		t.Fatalf("stock = %+v, want zero", got)
	}
}
