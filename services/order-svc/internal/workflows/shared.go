package workflows

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

const (
	// PaymentTimeout is the duration to wait for payment completion
	// 5 minutes for payment + 1 minute buffer for callback processing
	PaymentTimeout = 6 * time.Minute

	// ReservationHoldGrace covers webhook -> outbox -> Kafka -> ConfirmOrder for
	// a payment landing at the edge of PaymentTimeout, against inventory's 60s
	// expiry sweep. Happy path only; the retry-storm tail is backstopped by
	// inventory's Confirm re-acquiring a swept hold.
	// See docs/RESERVATION_HOLD.md.
	ReservationHoldGrace = 3 * time.Minute
)

// reservationExpiry returns the instant the inventory hold for an order must
// live until: the full payment window plus the grace that covers the
// post-payment confirmation chain.
func reservationExpiry(now time.Time) time.Time {
	return now.Add(PaymentTimeout + ReservationHoldGrace)
}

type Compensations struct {
	compensations []any
	arguments     [][]any
}

func (s *Compensations) AddCompensation(activity any, parameters ...any) {
	s.compensations = append(s.compensations, activity)
	s.arguments = append(s.arguments, parameters)
}

// Compensate runs the registered compensations newest-first, never concurrently:
// a later step's undo may need an earlier step's state still in place. Errors are
// logged and the loop continues, so one failed undo cannot block the rest.
func (s Compensations) Compensate(ctx workflow.Context) {
	logger := workflow.GetLogger(ctx)

	for i := len(s.compensations) - 1; i >= 0; i-- {
		errCompensation := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, getCompensationActivityOptions()),
			s.compensations[i],
			s.arguments[i]...,
		).Get(ctx, nil)

		if errCompensation != nil {
			logger.Error("Executing compensation failed", "Error", errCompensation)
		}
	}
}
