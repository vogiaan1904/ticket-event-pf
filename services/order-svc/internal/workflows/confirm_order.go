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

// markForRefund records that a paid order cannot be fulfilled: it moves the
// order to REFUND_REQUIRED and emits the event a refund process consumes.
// Both are best-effort and their failures are logged rather than returned --
// the caller is already failing the workflow, and losing the marking must not
// also lose the reason.
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

// freePurchaseSlot ends the buyer's claim on their one in-flight purchase. The
// claim is taken before the order exists and is what a duplicate create is
// answered with, so it has to be given back the moment the purchase has an
// outcome. Without that, an event with no waiting room keys the slot on the
// buyer and the event -- stable for that buyer's lifetime -- and every later
// purchase of the same event is answered with the finished order and its dead
// payment URL instead of a new checkout.
//
// A failure is logged rather than returned: the outcome it follows is already
// recorded, and failing the workflow over the claim would report a purchase
// that actually landed as a failed one. The claim's TTL is the backstop.
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

		return ErrOrderAlreadyProcessed
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

	// 5. Free the waiting room slot. The buyer already has their ticket, so a
	//    failure here is not the order's failure: it costs one slot until the
	//    waiting room's session TTL reclaims it. Returning the error would mark
	//    a fulfilled purchase as a failed workflow and page someone for it.
	if err := publishCheckoutCompleted(ctx, o.SessionID, o.UserID, o.EventID); err != nil {
		logger.Warn("Order is complete but the waiting room was not notified; the slot will be reclaimed by its session TTL",
			"error", err, "orderCode", o.Code, "sessionID", o.SessionID)
	}

	logger.Info("Order confirmed successfully", "orderCode", in.OrderCode)
	return nil
}
