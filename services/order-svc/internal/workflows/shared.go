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
	//
	// It is sized for the happy path, NOT for a retry storm: a ConfirmOrder
	// that exhausts getConfirmOrderActivityOptions' policy burns ~8 minutes of
	// backoff alone, well past this grace. That case is covered on the other
	// side -- inventory's Confirm re-acquires a hold the worker already swept,
	// as long as the stock has not been resold. This grace removes the common
	// race; that re-acquire is the backstop for the tail.
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

// Compensate runs the registered compensations newest-first. The order is the
// point: a later step's undo may depend on an earlier step's state still being
// there, so these cannot run concurrently and there is no flag to make them.
//
// Errors are logged and the loop continues. A compensation that fails must not
// prevent the remaining ones from running, and the caller is already on its
// way to failing the workflow.
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
