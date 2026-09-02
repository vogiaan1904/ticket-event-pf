package workflows

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
)

// By the time the notification is sent the buyer has their ticket: inventory is
// confirmed and the order is COMPLETED. A publish that fails costs a waiting
// room slot until its session TTL expires -- it does not un-sell the ticket, so
// it must not mark the workflow failed.
func TestConfirmOrder_PublishFailureDoesNotFailTheOrder(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("GetOrder", mock.Anything, mock.Anything).
		Return(&models.Order{
			Code: "TB-TEST-0001", Status: models.OrderStatusPending,
			SessionID: "sess-1", UserID: "u1", EventID: "e1",
		}, nil).Once()
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity("UpdateOrderStatus", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity("PublishCheckoutCompleted", mock.Anything, mock.Anything).
		Return(errors.New("broker unavailable"))

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{
		OrderCode: "TB-TEST-0001", Status: models.OrderStatusCompleted,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed notification failed the workflow: %v", err)
	}
}

// A second delivery of the same payment event must be a no-op, not a second
// confirm. Kafka is at-least-once and this workflow is its consumer.
func TestConfirmOrder_AlreadyCompletedIsANoOp(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("GetOrder", mock.Anything, mock.Anything).
		Return(&models.Order{Code: "TB-TEST-0001", Status: models.OrderStatusCompleted}, nil).Once()
	// Given zero calls, but must be mocked: an unmocked activity that IS
	// called runs the real zero-value struct and panics instead of failing
	// the AssertNotCalled below with a readable message.
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{
		OrderCode: "TB-TEST-0001", Status: models.OrderStatusCompleted,
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a redelivered payment event failed: %v", err)
	}
	env.AssertNotCalled(t, "ConfirmInventory", mock.Anything, mock.Anything)
}
