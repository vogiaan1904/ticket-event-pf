package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func getCreateOrderActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 2,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	}
}

func getConfirmOrderActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 5,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 2,
			MaximumAttempts:    10,
		},
	}
}

func getCompensationActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 2,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	}
}

// activityRetryBudget is the longest an activity can hold up a workflow before
// Temporal gives up on it: every attempt allowed its full start-to-close
// timeout, plus the backoff waited between them.
func activityRetryBudget(o workflow.ActivityOptions) time.Duration {
	attempts := int(o.RetryPolicy.MaximumAttempts)
	total := o.StartToCloseTimeout * time.Duration(attempts)

	interval := o.RetryPolicy.InitialInterval
	for range attempts - 1 {
		total += interval

		interval = time.Duration(float64(interval) * o.RetryPolicy.BackoffCoefficient)
		if interval > o.RetryPolicy.MaximumInterval {
			interval = o.RetryPolicy.MaximumInterval
		}
	}

	return total
}

// CreateOrderSlotBudget is the longest CreateOrder can run between the buyer's
// purchase-slot claim being written and the order row existing to point at.
// That span is two activities -- the inventory hold, then the order write --
// and the row only appears when the second of them lands, so each is counted at
// its whole retry budget.
//
// It is computed from the activity options rather than written down as a
// duration because those options are what actually bound the workflow: raise
// the retry policy and this has to widen with it, or a create still
// legitimately reserving inventory starts looking abandoned to the next
// request that asks.
func CreateOrderSlotBudget() time.Duration {
	return 2 * activityRetryBudget(getCreateOrderActivityOptions())
}
