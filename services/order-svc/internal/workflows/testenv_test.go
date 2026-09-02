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

// requireNotCalled exists because TestWorkflowEnvironment.AssertNotCalled ANDs
// four sub-checks together -- a dummy *testing.T pass for the workflow mock,
// then the activity mock, then the same pair again against the real t -- and
// Go's && short-circuits on the first false. An activity name never matches
// anything in the workflow mock, so that term is always true, which means the
// very next term, the dummy-T activity check, is the one that decides the
// outcome. When the activity actually was called, that term is false, the
// chain stops right there, and the two terms holding the real t never run: no
// t.Errorf, no failure, no matter what actually happened. Only the bool
// AssertNotCalled returns is trustworthy; this makes that value the thing
// that fails the test.
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
