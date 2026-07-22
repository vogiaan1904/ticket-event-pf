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

// The grace has to be big enough to cover webhook -> outbox -> Kafka ->
// ConfirmOrder, plus one full sweep of the inventory expiry worker (60s).
func TestReservationHoldGrace_CoversWorkerInterval(t *testing.T) {
	const workerInterval = time.Minute

	if ReservationHoldGrace <= workerInterval {
		t.Fatalf("ReservationHoldGrace = %v, must exceed the inventory expiry worker interval of %v",
			ReservationHoldGrace, workerInterval)
	}
}
