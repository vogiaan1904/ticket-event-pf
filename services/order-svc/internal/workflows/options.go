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
