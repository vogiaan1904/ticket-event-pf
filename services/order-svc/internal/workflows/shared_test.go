package workflows

import (
	"testing"
	"time"
)

// The inventory hold must outlive the payment window. If it does not, a
// payment completing at the edge of the window races the inventory expiry
// worker, and losing that race means a captured payment with no seat.
func TestReservationExpiry_OutlivesPaymentWindow(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	expiry := reservationExpiry(now)
	paymentDeadline := now.Add(PaymentTimeout)

	if !expiry.After(paymentDeadline) {
		t.Fatalf("hold expires at %s but the payment window closes at %s -- the hold must strictly outlive it",
			expiry.Format(time.RFC3339), paymentDeadline.Format(time.RFC3339))
	}
}

// Invariant: the grace is at least ten times the measured confirm-lag p99 (2.47s).
// See docs/plans/2026-11-14-a-late-payment-gets-its-ticket.md#ticket-5-results.
func TestReservationHoldGrace_CoversTenTimesConfirmLag(t *testing.T) {
	const minGrace = 30 * time.Second

	if ReservationHoldGrace < minGrace {
		t.Fatalf("ReservationHoldGrace = %v, must be at least %v (0036)", ReservationHoldGrace, minGrace)
	}
}
