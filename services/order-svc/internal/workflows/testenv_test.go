package workflows

import (
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
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
	// These tests mock short steps by name, which the test suite cannot do for a
	// local activity; short_steps_test.go covers the local path.
	env.OnGetVersion(shortStepsChangeID, workflow.DefaultVersion, 1).Return(workflow.DefaultVersion).Maybe()
	t.Cleanup(func() { env.AssertExpectations(t) })
	return env
}

// requireNotCalled fails on the bool AssertNotCalled returns, not on its own
// t.Errorf. That method ANDs four sub-checks and Go short-circuits: an activity
// name never matches the workflow mock, so the dummy-T activity check decides,
// and when the activity *was* called the two terms holding the real t never run
// -- no t.Errorf, no failure, whatever actually happened.
func requireNotCalled(t *testing.T, env *testsuite.TestWorkflowEnvironment, name string, args ...interface{}) {
	t.Helper()
	if env.AssertNotCalled(t, name, args...) {
		return
	}
	t.Fatalf("%s was called but must not have been", name)
}

func TestHarness_RegistersActivities(t *testing.T) {
	env := newTestEnv(t)
	if env == nil {
		t.Fatal("expected a test workflow environment")
	}
}
