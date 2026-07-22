package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
)

func reserveSvc(t *testing.T) (ReservationService, *pkgGorm.Repository) {
	repo := newTestDB(t)
	return NewReservationService(newTestLogger(), repo), repo
}

func ticketClassByID(t *testing.T, repo *pkgGorm.Repository, id int64) models.TicketClass {
	t.Helper()
	var tc models.TicketClass
	if err := repo.FindByID(context.Background(), &tc, id); err != nil {
		t.Fatalf("reload ticket class %d: %v", id, err)
	}
	return tc
}

func TestReserve_SingleItem_IncrementsReserved(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "order-1",
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 3}},
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 3 {
		t.Fatalf("reserved = %d, want 3", got.Reserved)
	}
}

// The capacity-leak test: a two-item order where the second item cannot be
// satisfied must leave BOTH ticket classes untouched (fully atomic).
func TestReserve_PartialFailure_NoCapacityLeak(t *testing.T) {
	svc, repo := reserveSvc(t)
	tcOK := seedTicketClass(t, repo, 100, 0, 0)
	tcFull := seedTicketClass(t, repo, 1, 0, 0)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "order-2",
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		Items: []ReserveItem{
			{TicketClassID: tcOK.ID, Qty: 2},
			{TicketClassID: tcFull.ID, Qty: 5}, // impossible
		},
	})
	if err == nil {
		t.Fatal("expected Reserve to fail on insufficient stock")
	}
	if got := ticketClassByID(t, repo, tcOK.ID); got.Reserved != 0 {
		t.Fatalf("tcOK.reserved = %d, want 0 (no leak)", got.Reserved)
	}
	var count int64
	repo.WithContext(context.Background()).Model(&models.Reservation{}).
		Where("order_code = ?", "order-2").Count(&count)
	if count != 0 {
		t.Fatalf("reservation rows = %d, want 0", count)
	}
}

func TestReserve_Idempotent_SecondCallNoOp(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	in := ReserveInput{
		OrderCode: "order-3",
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 4}},
	}
	if err := svc.Reserve(context.Background(), in); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	if err := svc.Reserve(context.Background(), in); err != nil {
		t.Fatalf("second Reserve should be a no-op, got: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 4 {
		t.Fatalf("reserved = %d, want 4 (not double-counted)", got.Reserved)
	}
}

func TestReserve_Concurrent_NoOversell(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 10, 0, 0) // capacity 10

	var wg sync.WaitGroup
	var okCount int64
	var mu sync.Mutex
	for i := 0; i < 20; i++ { // 20 orders each wanting 1
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := svc.Reserve(context.Background(), ReserveInput{
				OrderCode: "cc-" + string(rune('a'+n)),
				ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
				Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
			})
			if err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved > got.Total {
		t.Fatalf("OVERSOLD: reserved=%d > total=%d", got.Reserved, got.Total)
	}
	if okCount != 10 {
		t.Fatalf("successful reserves = %d, want exactly 10", okCount)
	}
}

// Reserve -> Release -> Reserve must not report success while holding
// nothing. The old count-any-status short-circuit saw the CANCELLED rows and
// no-oped, handing back an order with zero inventory behind it.
func TestReserve_AfterRelease_ReturnsConflict(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	in := ReserveInput{
		OrderCode: "o-rereserve",
		ExpiresAt: future(),
		Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 4}},
	}
	must(t, svc.Reserve(context.Background(), in))
	must(t, svc.Release(context.Background(), "o-rereserve"))

	if err := svc.Reserve(context.Background(), in); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Reserve after Release = %v, want ErrStateConflict", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0 (the rejected re-reserve must hold nothing)", got.Reserved)
	}
}

func TestReserve_AfterExpiry_ReturnsConflict(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	in := ReserveInput{
		OrderCode: "o-reexpire",
		ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 4}},
	}
	must(t, svc.Reserve(context.Background(), in))
	if _, err := svc.BatchExpireReservations(context.Background(), 500); err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}

	if err := svc.Reserve(context.Background(), in); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Reserve after expiry = %v, want ErrStateConflict", err)
	}
}
