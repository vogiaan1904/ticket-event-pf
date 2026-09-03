package workflows

import (
	"errors"
	"fmt"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func GetConfirmOrderWorkflowID(oCode string) string {
	return fmt.Sprintf("ConfirmOrder:%s", oCode)
}

// markForRefund records that a paid order cannot be fulfilled: REFUND_REQUIRED
// plus the event a refund process consumes. Both are best-effort -- the caller
// is already failing the workflow, and losing the marking must not lose the
// reason too.
func markForRefund(ctx workflow.Context, o *models.Order, reason string) {
	logger := workflow.GetLogger(ctx)
	logger.Error("Order is paid but cannot be fulfilled", "orderCode", o.Code, "reason", reason)

	if err := updateOrderStatus(ctx, o.Code, models.OrderStatusRefundRequired); err != nil {
		logger.Error("Failed to mark the order for refund", "error", err, "orderCode", o.Code)
	}
	if err := publishRefundRequired(ctx, o, reason); err != nil {
		logger.Error("Failed to publish the refund-required event", "error", err, "orderCode", o.Code)
	}
	freePurchaseSlot(ctx, o)
}

// freePurchaseSlot ends the buyer's claim once the purchase has an outcome.
// Holding it is a lockout, not a leak: without a waiting room the key is stable
// per buyer and event, so every later purchase would be answered with the
// finished order. Failure is logged, not returned -- the TTL is the backstop.
// See docs/PURCHASE_SLOT.md#release.
func freePurchaseSlot(ctx workflow.Context, o *models.Order) {
	if err := releasePurchaseSlot(ctx, o); err != nil {
		workflow.GetLogger(ctx).Error("Failed to release the buyer's purchase slot; it stays held until its TTL expires",
			"error", err, "orderCode", o.Code)
	}
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

		switch o.Status {
		case models.OrderStatusCompleted:
			return nil
		case models.OrderStatusCancelled, models.OrderStatusPaymentFailed, models.OrderStatusTimeout:
			// The payment landed on an order that had already been
			// cancelled, timed out or failed. The money is real; the order
			// will not be fulfilled.
			markForRefund(ctx, o, "payment settled on an order in status "+string(o.Status))
		case models.OrderStatusRefundRequired, models.OrderStatusRefunded:
			// A redelivered payment event. REFUND_REQUIRED already recorded
			// the debt and REFUNDED already settled it, so marking again
			// would overwrite a completed refund with one still owed --
			// redelivery of either must be a pure no-op.
		default:
			// A status this switch does not recognise. Guessing would risk
			// marking a healthy order for refund, or silently letting a real
			// one go unmarked, so nothing is written and the event is simply
			// refused.
			logger.Error("Order is in an unrecognised status; refusing to guess whether it needs a refund",
				"orderCode", o.Code, "status", o.Status)
		}

		return NewOrderAlreadyProcessedError()
	}

	// 2. Confirm inventory
	if err := confirmInventory(ctx, in.OrderCode); err != nil {
		logger.Error("Failed to confirm inventory", "error", err)

		var appErr *temporal.ApplicationError
		if errors.As(err, &appErr) && appErr.Type() == order.ErrTypeInventoryCannotConfirm {
			// Inventory could not turn the hold into a sale -- typically the
			// expiry worker released it and the stock was resold before the
			// confirmation arrived. Retrying cannot change that outcome.
			markForRefund(ctx, o, "inventory could not be confirmed: "+err.Error())
		}
		// Any other failure is an infrastructure fault (unavailable, timed
		// out) rather than a business rejection -- whether the order can
		// still be fulfilled is unknown, so it is left PENDING for a retried
		// payment event instead of marked for a refund it may not need.
		return err
	}

	// 3. Update order status to COMPLETED
	if err := updateOrderStatus(ctx, o.Code, models.OrderStatusCompleted); err != nil {
		return err
	}

	// 4. Give the buyer's purchase slot back. The purchase is over, so the
	//    claim that suppressed a duplicate create has nothing left to guard.
	freePurchaseSlot(ctx, o)

	// 5. Free the waiting room slot. The ticket is already the buyer's, so a
	//    failure costs one slot until the session TTL, not the order.
	if err := publishCheckoutCompleted(ctx, o.SessionID, o.UserID, o.EventID); err != nil {
		logger.Warn("Order is complete but the waiting room was not notified; the slot will be reclaimed by its session TTL",
			"error", err, "orderCode", o.Code, "sessionID", o.SessionID)
	}

	logger.Info("Order confirmed successfully", "orderCode", in.OrderCode)
	return nil
}
