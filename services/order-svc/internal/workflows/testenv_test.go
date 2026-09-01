package workflows

import (
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"go.temporal.io/sdk/testsuite"
)

// The activity structs are registered as zero values: the test environment
// dispatches by name and every call is mocked with OnActivity, so no
// dependency behind them is ever reached.
func newTestEnv(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterActivity(&activities.InventoryActivities{})
	env.RegisterActivity(&activities.OrderActivities{})
	env.RegisterActivity(&activities.PaymentActivities{})
	env.RegisterActivity(&activities.EventPublishingActivities{})
	t.Cleanup(func() { env.AssertExpectations(t) })
	return env
}

func TestHarness_RegistersActivities(t *testing.T) {
	env := newTestEnv(t)
	if env == nil {
		t.Fatal("expected a test workflow environment")
	}
}
