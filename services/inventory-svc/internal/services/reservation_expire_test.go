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
