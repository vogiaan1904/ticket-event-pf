package temporal

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// NewOrderWorker creates a worker for order workflows with the specified task queue
func NewOrderWorker(c client.Client, taskQueue string) worker.Worker {
	return worker.New(c, taskQueue, worker.Options{
		MaxConcurrentWorkflowTaskExecutionSize: 100,
		MaxConcurrentActivityExecutionSize:     100,
	})
}
