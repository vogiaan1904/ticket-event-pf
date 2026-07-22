package workflows

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

const (
	// PaymentTimeout is the duration to wait for payment completion
	// 5 minutes for payment + 1 minute buffer for callback processing
	PaymentTimeout = 6 * time.Minute

	// ReservationHoldGrace extends the inventory hold beyond PaymentTimeout.
	// A payment completing at the very edge of the window still has to travel
	// webhook -> outbox -> Kafka -> ConfirmOrder, and inventory's expiry
	// worker sweeps every 60s. Without this slack the worker wins that race
	// and the order ends up paid with no seat behind it.
	ReservationHoldGrace = 3 * time.Minute

	// SignalNamePaymentCompleted is the signal name for payment completion
	SignalNamePaymentCompleted = "payment-completed"

	// SignalNamePaymentFailed is the signal name for payment failure
	SignalNamePaymentFailed = "payment-failed"
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

func (s Compensations) Compensate(ctx workflow.Context, inParallel bool) {
	logger := workflow.GetLogger(ctx)

	if !inParallel {
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
}
