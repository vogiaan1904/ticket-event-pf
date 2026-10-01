package workflows

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"go.temporal.io/sdk/temporal"
)

func timedOut(code, session string) activities.ExpireOrderResult {
	return activities.ExpireOrderResult{Expired: true, Order: &models.Order{
		Code: code, Status: models.OrderStatusTimeout, SessionID: session, UserID: "u1", EventID: "e1",
	}}
}

func TestExpireOrder_AnUnpaidOrderGivesBackEverythingItHeld(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("ExpireOrder", mock.Anything, "TB-EXP-0001").Return(timedOut("TB-EXP-0001", "sess-1"), nil).Once()
	env.OnActivity("ReleaseInventory", mock.Anything, "TB-EXP-0001").Return(nil).Once()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, "sess-1", "TB-EXP-0001").Return(nil).Once()
	env.OnActivity("PublishCheckoutExpired", mock.Anything, activities.PublishCheckoutExpiredInput{
		SessionID: "sess-1", UserID: "u1", EventID: "e1",
	}).Return(nil).Once()

	env.ExecuteWorkflow(ExpireOrder, &ExpireOrderWorkflowInput{OrderCode: "TB-EXP-0001"})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("completed = %v, err = %v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
}

// Paid, failed or cancelled first: the timer has nothing left to do.
func TestExpireOrder_ASettledOrderIsLeftAlone(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("ExpireOrder", mock.Anything, "TB-EXP-0002").Return(activities.ExpireOrderResult{
		Order: &models.Order{Code: "TB-EXP-0002", Status: models.OrderStatusCompleted, SessionID: "sess-2"},
	}, nil).Once()
	env.OnActivity("ReleaseInventory", mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("PublishCheckoutExpired", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ExpireOrder, &ExpireOrderWorkflowInput{OrderCode: "TB-EXP-0002"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	requireNotCalled(t, env, "ReleaseInventory", mock.Anything, mock.Anything)
	requireNotCalled(t, env, "ReleasePurchaseSlot", mock.Anything, mock.Anything, mock.Anything)
	requireNotCalled(t, env, "PublishCheckoutExpired", mock.Anything, mock.Anything)
}

// The hold expires on its own anyway; the chair must not wait on it.
func TestExpireOrder_AFailedReleaseStillFreesTheChair(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("ExpireOrder", mock.Anything, "TB-EXP-0003").Return(timedOut("TB-EXP-0003", "sess-3"), nil).Once()
	env.OnActivity("ReleaseInventory", mock.Anything, "TB-EXP-0003").
		Return(temporal.NewNonRetryableApplicationError("inventory down", "Unavailable", errors.New("inventory down"))).Once()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, "sess-3", "TB-EXP-0003").Return(nil).Once()
	env.OnActivity("PublishCheckoutExpired", mock.Anything, mock.Anything).Return(nil).Once()

	env.ExecuteWorkflow(ExpireOrder, &ExpireOrderWorkflowInput{OrderCode: "TB-EXP-0003"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed release failed the timeout: %v", err)
	}
}

func TestExpireOrder_WithoutAWaitingRoomNothingIsPublished(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("ExpireOrder", mock.Anything, "TB-EXP-0004").Return(timedOut("TB-EXP-0004", ""), nil).Once()
	env.OnActivity("ReleaseInventory", mock.Anything, "TB-EXP-0004").Return(nil).Once()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, "user#u1:event#e1", "TB-EXP-0004").Return(nil).Once()
	env.OnActivity("PublishCheckoutExpired", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ExpireOrder, &ExpireOrderWorkflowInput{OrderCode: "TB-EXP-0004"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	requireNotCalled(t, env, "PublishCheckoutExpired", mock.Anything, mock.Anything)
}
