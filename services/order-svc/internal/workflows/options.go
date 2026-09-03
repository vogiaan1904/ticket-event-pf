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

// activityRetryBudget is the longest an activity can hold up a workflow: every
// attempt at its full start-to-close timeout, plus the backoff between them.
func activityRetryBudget(o workflow.ActivityOptions) time.Duration {
	attempts := int(o.RetryPolicy.MaximumAttempts)
	total := o.StartToCloseTimeout * time.Duration(attempts)

	interval := o.RetryPolicy.InitialInterval
	for range attempts - 1 {
		total += interval
		interval = min(time.Duration(float64(interval)*o.RetryPolicy.BackoffCoefficient), o.RetryPolicy.MaximumInterval)
	}

	return total
}

// CreateOrderSlotBudget is the longest CreateOrder can run between the claim
// being written and the order row existing: two activities at full retry budget,
// the inventory hold then the order write. Derived from the options rather than
// hardcoded so raising the retry policy widens it too.
// See docs/PURCHASE_SLOT.md#settle-window.
func CreateOrderSlotBudget() time.Duration {
	return 2 * activityRetryBudget(getCreateOrderActivityOptions())
}
