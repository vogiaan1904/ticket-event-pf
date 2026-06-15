package workflows

import (
	"fmt"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"go.temporal.io/sdk/workflow"
)

func GetConfirmOrderWorkflowID(oCode string) string {
	return fmt.Sprintf("ConfirmOrder:%s", oCode)
}

type ConfirmOrderWorkflowInput struct {
	OrderCode string
	Status    models.OrderStatus
}

// ProcessPostPaymentOrder handles the post-payment phase of order processing
func ConfirmOrder(ctx workflow.Context, in *ConfirmOrderWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting confirm order workflow", "orderCode", in.OrderCode)

	ctx = workflow.WithActivityOptions(ctx, getConfirmOrderActivityOptions())

	// 1. Validate order
	o, err := validateOrder(ctx, in.OrderCode)
	if err != nil {
		return err
	}

	if o.Status != models.OrderStatusPending {
		logger.Warn("Order already processed", "orderCode", in.OrderCode, "status", o.Status)
		if o.Status == models.OrderStatusCompleted {
			return nil
		}
		return ErrOrderAlreadyProcessed
	}

	// 2. Confirm inventory
	if err := confirmInventory(ctx, in.OrderCode); err != nil {
		logger.Error("Failed to confirm inventory", "error", err)
		// This is a critical error - we need manual intervention
		// TODO: Implement manual intervention task creation or alerting
		return err
	}

	// 3. Update order status to COMPLETED
	if err := updateOrderStatus(ctx, o.Code, models.OrderStatusCompleted); err != nil {
		// This is a critical error - we need manual intervention
		// TODO: Implement manual intervention task creation or alerting
		return err
	}

	// 4. Publish checkout completed event to free waitroom slot
	if err := publishCheckoutCompleted(ctx, o.SessionID, o.UserID, o.EventID); err != nil {
		logger.Warn("Failed to publish checkout completed event", "error", err, "sessionID", o.SessionID)
		return err
	}

	logger.Info("Order confirmed successfully", "orderCode", in.OrderCode)
	return nil
}
