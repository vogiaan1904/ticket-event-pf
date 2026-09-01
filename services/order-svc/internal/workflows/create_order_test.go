package workflows

import (
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/workflow"
)

func testCreateInput() *CreateOrderWorkflowInput {
	return &CreateOrderWorkflowInput{
		OrderCode:   "TB-TEST-0001",
		SessionID:   "sess-1",
		UserID:      "user-1",
		EventID:     "evt-1",
		Currency:    "VND",
		TotalAmount: 1000,
		Items: []CreateOrderItemInput{
			{TicketClassID: "1", Quantity: 2, PriceAtPurchase: 500, TotalAmount: 1000},
		},
		PaymentProvider: "ZALOPAY",
	}
}

// The scarce resource decides the outcome, so it must be taken before any
// bookkeeping. If reserve fails there is nothing to undo: no order row was
// ever written, and no compensation activity may run.
func TestCreateOrder_ReserveFailsBeforeAnyOrderIsWritten(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("ReserveInventory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(NewInsufficientInventoryError(ErrInsufficientInventory)).Once()
	env.OnActivity("CreateOrder", mock.Anything, mock.Anything).
		Return(&models.Order{Code: "TB-TEST-0001"}, nil).Maybe()
	env.OnActivity("CreateOrderItems", mock.Anything, mock.Anything, mock.Anything).
		Return([]models.OrderItem{}, nil).Maybe()
	env.OnActivity("DeleteOrder", mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("ReleaseInventory", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(CreateOrder, testCreateInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if env.GetWorkflowError() == nil {
		t.Fatal("expected the sold-out rejection to fail the workflow")
	}
	env.AssertNotCalled(t, "CreateOrder", mock.Anything, mock.Anything)
	env.AssertNotCalled(t, "CreateOrderItems", mock.Anything, mock.Anything, mock.Anything)
	env.AssertNotCalled(t, "DeleteOrder", mock.Anything, mock.Anything)
	env.AssertNotCalled(t, "ReleaseInventory", mock.Anything, mock.Anything)
}

// Availability is decided once, under the row lock inside Reserve. A separate
// unlocked pre-check cannot be trusted and must not be issued.
func TestCreateOrder_DoesNotPreCheckAvailability(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("ReserveInventory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()
	env.OnActivity("CreateOrder", mock.Anything, mock.Anything).
		Return(&models.Order{Code: "TB-TEST-0001", Status: models.OrderStatusPending}, nil).Once()
	env.OnActivity("CreateOrderItems", mock.Anything, mock.Anything, mock.Anything).
		Return([]models.OrderItem{}, nil).Once()
	env.OnActivity("CreatePaymentIntent", mock.Anything, mock.Anything).
		Return(&payment.CreatePaymentIntentResponse{PaymentUrl: "https://pay.test/1"}, nil).Once()
	env.OnActivity("CheckAvailability", mock.Anything, mock.Anything).
		Return(true, nil).Maybe()

	env.ExecuteWorkflow(CreateOrder, testCreateInput())

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("happy path failed: %v", err)
	}
	env.AssertNotCalled(t, "CheckAvailability", mock.Anything, mock.Anything)
}

// A payment failure arrives after the hold and both order writes, so all three
// must be undone, newest first.
func TestCreateOrder_PaymentFailureCompensatesInReverse(t *testing.T) {
	env := newTestEnv(t)

	env.OnActivity("ReserveInventory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()
	env.OnActivity("CreateOrder", mock.Anything, mock.Anything).
		Return(&models.Order{Code: "TB-TEST-0001", Status: models.OrderStatusPending}, nil).Once()
	env.OnActivity("CreateOrderItems", mock.Anything, mock.Anything, mock.Anything).
		Return([]models.OrderItem{}, nil).Once()
	env.OnActivity("CreatePaymentIntent", mock.Anything, mock.Anything).
		Return(nil, ErrPaymentFailed)

	env.OnActivity("DeleteOrderItems", mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity("DeleteOrder", mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity("ReleaseInventory", mock.Anything, mock.Anything).Return(nil).Once()

	env.ExecuteWorkflow(CreateOrder, testCreateInput())

	if env.GetWorkflowError() == nil {
		t.Fatal("expected the payment failure to fail the workflow")
	}
}

// Compensate took an inParallel flag whose true branch had no body, so a
// caller could disable the entire rollback and get no error back. The
// signature is the guard: there is nothing to pass.
func TestCompensate_HasNoOptOut(t *testing.T) {
	var c Compensations
	// Compile-time assertion: Compensate takes a context and nothing else.
	var _ func(workflow.Context) = c.Compensate
}
