package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
)

// seedTicketClassWindow seeds a ticket class with an explicit status and sale
// window. Passing nil for a bound means "unbounded on that side".
func seedTicketClassWindow(t *testing.T, repo *pkgGorm.Repository, status models.TicketClassStatus, start, end *time.Time) models.TicketClass {
	t.Helper()
	n := seedCounter.Add(1)
	tc := models.TicketClass{
		EventID:     fmt.Sprintf("evt-%s-%d", t.Name(), n),
		Name:        fmt.Sprintf("GA-%s-%d", t.Name(), n),
		PriceCents:  1000,
		Currency:    "USD",
		Total:       100,
		Status:      status,
		SaleStartAt: start,
		SaleEndAt:   end,
	}
	if err := repo.Create(context.Background(), &tc); err != nil {
		t.Fatalf("seed ticket class: %v", err)
	}
	return tc
}

func TestReserve_InactiveTicketClass_ReturnsSaleClosed(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusInactive, nil, nil)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-inactive", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
	})
	if !errors.Is(err, ErrSaleClosed) {
		t.Fatalf("Reserve on INACTIVE class = %v, want ErrSaleClosed", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0", got.Reserved)
	}
}

func TestReserve_BeforeSaleStart_ReturnsSaleClosed(t *testing.T) {
	svc, repo := reserveSvc(t)
	start := time.Now().UTC().Add(1 * time.Hour)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusActive, &start, nil)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-early", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
	})
	if !errors.Is(err, ErrSaleClosed) {
		t.Fatalf("Reserve before sale start = %v, want ErrSaleClosed", err)
	}
}

func TestReserve_AfterSaleEnd_ReturnsSaleClosed(t *testing.T) {
	svc, repo := reserveSvc(t)
	end := time.Now().UTC().Add(-1 * time.Hour)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusActive, nil, &end)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-late", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
	})
	if !errors.Is(err, ErrSaleClosed) {
		t.Fatalf("Reserve after sale end = %v, want ErrSaleClosed", err)
	}
}

func TestReserve_WithinSaleWindow_Succeeds(t *testing.T) {
	svc, repo := reserveSvc(t)
	start := time.Now().UTC().Add(-1 * time.Hour)
	end := time.Now().UTC().Add(1 * time.Hour)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusActive, &start, &end)

	if err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-inwindow", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 2}},
	}); err != nil {
		t.Fatalf("Reserve within the sale window: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 2 {
		t.Fatalf("reserved = %d, want 2", got.Reserved)
	}
}

func TestCheckAvailability_InactiveTicketClass_Rejects(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusInactive, nil, nil)

	ok, err := tcSvc.CheckAvailability(context.Background(), []CheckAvailabilityInput{
		{TicketClassID: tc.ID, Qty: 1},
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if ok {
		t.Fatal("accept = true, want false for an INACTIVE ticket class")
	}
}
