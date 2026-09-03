package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"testing"

	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
)

func updateSvc(t *testing.T) (TicketClassService, ReservationService, *pkgGorm.Repository) {
	t.Helper()
	repo := newTestDB(t)
	return NewTicketClassService(newTestLogger(), repo),
		NewReservationService(newTestLogger(), repo),
		repo
}

// A rename must not touch total, currency, or the sale window -- an
// unconditional field assignment wiped total to 0 and currency to "".
func TestUpdate_PartialUpdate_PreservesUnsetFields(t *testing.T) {
	tcSvc, _, repo := updateSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)

	name := "VIP"
	got, err := tcSvc.Update(context.Background(), tc.ID, UpdateTicketClassInput{Name: &name})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Name != "VIP" {
		t.Fatalf("name = %q, want VIP", got.Name)
	}
	if got.Total != 100 {
		t.Fatalf("total = %d, want 100 (must not be reset by a partial update)", got.Total)
	}
	if got.Currency != "USD" {
		t.Fatalf("currency = %q, want USD (must not be reset by a partial update)", got.Currency)
	}
}

// The P0 regression guard. Under the old Save()-based Update, reservations
// committing between the read and the write were erased from the counter.
func TestUpdate_ConcurrentReserve_DoesNotLoseHolds(t *testing.T) {
	tcSvc, rSvc, repo := updateSvc(t)
	tc := seedTicketClass(t, repo, 500, 0, 0)

	const reserves = 40
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount := 0
	var updateErrs []error

	for i := 0; i < reserves; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := rSvc.Reserve(context.Background(), ReserveInput{
				OrderCode: fmt.Sprintf("o-upd-race-%d", n),
				ExpiresAt: future(),
				Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
			})
			if err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("rename-%d", n)
			_, err := tcSvc.Update(context.Background(), tc.ID, UpdateTicketClassInput{Name: &name})
			if err != nil {
				mu.Lock()
				updateErrs = append(updateErrs, err)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	for _, err := range updateErrs {
		t.Errorf("concurrent Update: %v", err)
	}

	// 500 total against 40 single-seat reserves: all must fit, so any partial
	// success count is itself a defect (lost holds, spurious conflicts).
	if okCount != reserves {
		t.Fatalf("successful reserves = %d, want %d (all should have fit within total=500)", okCount, reserves)
	}

	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != okCount {
		t.Fatalf("reserved = %d but %d reserves succeeded -- holds were lost by a concurrent Update", got.Reserved, okCount)
	}

	// At least one concurrent Update must have committed, or the asserts
	// above pass vacuously.
	if !regexp.MustCompile(`^rename-\d+$`).MatchString(got.Name) {
		t.Fatalf("name = %q, want it to match rename-<n> (no concurrent Update appears to have committed)", got.Name)
	}
}

func TestUpdate_TotalBelowCommitted_ReturnsConflict(t *testing.T) {
	tcSvc, _, repo := updateSvc(t)
	tc := seedTicketClass(t, repo, 100, 5, 3)

	newTotal := 4
	_, err := tcSvc.Update(context.Background(), tc.ID, UpdateTicketClassInput{Total: &newTotal})
	if !errors.Is(err, ErrStateConflict) {
		t.Fatalf("shrinking total below reserved+sold = %v, want ErrStateConflict", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Total != 100 {
		t.Fatalf("total = %d, want 100 (rejected update must not apply)", got.Total)
	}
}

func TestUpdate_TotalAtCommitted_Succeeds(t *testing.T) {
	tcSvc, _, repo := updateSvc(t)
	tc := seedTicketClass(t, repo, 100, 5, 3)

	newTotal := 8 // exactly reserved + sold
	got, err := tcSvc.Update(context.Background(), tc.ID, UpdateTicketClassInput{Total: &newTotal})
	if err != nil {
		t.Fatalf("Update to exactly reserved+sold: %v", err)
	}
	if got.Total != 8 {
		t.Fatalf("total = %d, want 8", got.Total)
	}

	// Assert on the committed row, not the returned value: a Save()-based
	// read-modify-write hands back the counters it read, so `got` alone would
	// not catch the update path overwriting columns it must not touch.
	reloaded := ticketClassByID(t, repo, tc.ID)
	if reloaded.Reserved != 5 {
		t.Fatalf("reserved = %d, want 5 (must survive an unrelated Total update)", reloaded.Reserved)
	}
	if reloaded.Sold != 3 {
		t.Fatalf("sold = %d, want 3 (must survive an unrelated Total update)", reloaded.Sold)
	}
}

func TestUpdate_Unknown_ReturnsNotFound(t *testing.T) {
	tcSvc, _, _ := updateSvc(t)
	name := "ghost"
	if _, err := tcSvc.Update(context.Background(), 999999999, UpdateTicketClassInput{Name: &name}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update on unknown id = %v, want ErrNotFound", err)
	}
}
