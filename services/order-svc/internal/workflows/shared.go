package workflows

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

const (
	// PaymentTimeout is the duration to wait for payment completion
	// 5 minutes for payment + 1 minute buffer for callback processing
	PaymentTimeout = 6 * time.Minute

	// SignalNamePaymentCompleted is the signal name for payment completion
	SignalNamePaymentCompleted = "payment-completed"

	// SignalNamePaymentFailed is the signal name for payment failure
	SignalNamePaymentFailed = "payment-failed"
)

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
