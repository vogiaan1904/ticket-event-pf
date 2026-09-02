package workflows

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	requireNotCalled(t, env, "ConfirmInventory", mock.Anything, mock.Anything)
}

// The payment succeeded and the stock is gone. The buyer is owed money, so the
// order must stop claiming to be PENDING and the fact must leave the service
// as an event -- not a log line and a TODO.
func TestConfirmOrder_UnfulfillableOrderIsMarkedForRefund(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("GetOrder", mock.Anything, mock.Anything).
		Return(&models.Order{
			Code: "TB-TEST-0002", Status: models.OrderStatusPending,
			SessionID: "sess-2", UserID: "u2", EventID: "e2",
		}, nil).Once()
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).
		Return(temporal.NewNonRetryableApplicationError(
			"stock is gone", order.ErrTypeInventoryCannotConfirm, errors.New("stock is gone"),
		)).Once()
	env.OnActivity("UpdateOrderStatus", mock.Anything, mock.Anything, models.OrderStatusRefundRequired).
		Return(nil).Once()
	env.OnActivity("PublishRefundRequired", mock.Anything, mock.Anything).Return(nil).Once()

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{
		OrderCode: "TB-TEST-0002", Status: models.OrderStatusCompleted,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if env.GetWorkflowError() == nil {
		t.Fatal("an unfulfillable paid order must still fail the workflow so it is visible")
	}
}

// ConfirmInventory failing on an infrastructure fault -- the gRPC call itself
// could not be completed -- says nothing about whether the order can still be
// fulfilled. It must fail the workflow so the payment event is retried, but it
// must not be marked for refund: that would pay back a buyer whose ticket may
// still be confirmable once inventory-svc is reachable again.
func TestConfirmOrder_InventoryInfrastructureFailureIsNotMarkedForRefund(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("GetOrder", mock.Anything, mock.Anything).
		Return(&models.Order{
			Code: "TB-TEST-0006", Status: models.OrderStatusPending,
			SessionID: "sess-6", UserID: "u6", EventID: "e6",
		}, nil).Once()
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).
		Return(status.Error(codes.Unavailable, "inventory-svc unreachable"))
	env.OnActivity("UpdateOrderStatus", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("PublishRefundRequired", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{
		OrderCode: "TB-TEST-0006", Status: models.OrderStatusCompleted,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if env.GetWorkflowError() == nil {
		t.Fatal("an inventory outage must still fail the workflow so the payment event is retried")
	}
	requireNotCalled(t, env, "UpdateOrderStatus", mock.Anything, mock.Anything, mock.Anything)
	requireNotCalled(t, env, "PublishRefundRequired", mock.Anything, mock.Anything)
}

// A payment event lands on an order already cancelled or timed out. The money
// is real and the order will never be fulfilled, so this branch of the status
// guard -- not just the inventory-confirm branch -- must also mark it for
// refund and still fail the workflow.
func TestConfirmOrder_PaymentOnATerminalOrderIsMarkedForRefund(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("GetOrder", mock.Anything, mock.Anything).
		Return(&models.Order{
			Code: "TB-TEST-0007", Status: models.OrderStatusCancelled,
			UserID: "u7", EventID: "e7",
		}, nil).Once()
	env.OnActivity("UpdateOrderStatus", mock.Anything, mock.Anything, models.OrderStatusRefundRequired).
		Return(nil).Once()
	env.OnActivity("PublishRefundRequired", mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{
		OrderCode: "TB-TEST-0007", Status: models.OrderStatusCompleted,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if env.GetWorkflowError() == nil {
		t.Fatal("a payment landing on a terminal order must still fail the workflow")
	}
	requireNotCalled(t, env, "ConfirmInventory", mock.Anything, mock.Anything)
}

// A redelivered payment event for an order already marked REFUND_REQUIRED
// must not rewrite the status or publish the event again -- the debt is
// already recorded, so redelivery has to be idempotent, not cumulative.
func TestConfirmOrder_RedeliveredPaymentOnRefundRequiredOrderIsANoOp(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("GetOrder", mock.Anything, mock.Anything).
		Return(&models.Order{Code: "TB-TEST-0008", Status: models.OrderStatusRefundRequired}, nil).Once()
	env.OnActivity("UpdateOrderStatus", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("PublishRefundRequired", mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{
		OrderCode: "TB-TEST-0008", Status: models.OrderStatusCompleted,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if env.GetWorkflowError() == nil {
		t.Fatal("a redelivered event on an order already needing refund must still report failure")
	}
	requireNotCalled(t, env, "UpdateOrderStatus", mock.Anything, mock.Anything, mock.Anything)
	requireNotCalled(t, env, "PublishRefundRequired", mock.Anything, mock.Anything)
	requireNotCalled(t, env, "ConfirmInventory", mock.Anything, mock.Anything)
}

// A redelivered payment event for an order that has already been refunded
// must not drag it back to REFUND_REQUIRED -- the money has already gone
// back, and re-marking it would misreport a settled refund as one still owed.
func TestConfirmOrder_RedeliveredPaymentOnRefundedOrderIsANoOp(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("GetOrder", mock.Anything, mock.Anything).
		Return(&models.Order{Code: "TB-TEST-0009", Status: models.OrderStatusRefunded}, nil).Once()
	env.OnActivity("UpdateOrderStatus", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("PublishRefundRequired", mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{
		OrderCode: "TB-TEST-0009", Status: models.OrderStatusCompleted,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if env.GetWorkflowError() == nil {
		t.Fatal("a redelivered event on a refunded order must still report failure")
	}
	requireNotCalled(t, env, "UpdateOrderStatus", mock.Anything, mock.Anything, mock.Anything)
	requireNotCalled(t, env, "PublishRefundRequired", mock.Anything, mock.Anything)
	requireNotCalled(t, env, "ConfirmInventory", mock.Anything, mock.Anything)
}
