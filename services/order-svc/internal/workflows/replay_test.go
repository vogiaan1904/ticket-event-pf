package workflows

import (
	"testing"

	"go.temporal.io/sdk/worker"
)

// TestReplay_RecordedHistories replays sagas recorded on k3s against today's code.
// A change that alters the commands a workflow emits, unguarded by
// workflow.GetVersion, fails here instead of on the sagas in flight at deploy.
func TestReplay_RecordedHistories(t *testing.T) {
	r := worker.NewWorkflowReplayer()
	r.RegisterWorkflow(CreateOrder)
	r.RegisterWorkflow(ConfirmOrder)

	for _, f := range []string{
		"testdata/create_order.json", "testdata/confirm_order.json",
		"testdata/create_order_v1.json", "testdata/confirm_order_v1.json",
	} {
		if err := r.ReplayWorkflowHistoryFromJSONFile(nil, f); err != nil {
			t.Errorf("replay %s: %v", f, err)
		}
	}
}
