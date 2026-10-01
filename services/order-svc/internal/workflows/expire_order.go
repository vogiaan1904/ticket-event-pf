package workflows

import (
	"fmt"

	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"go.temporal.io/sdk/workflow"
)

func GetExpireOrderWorkflowID(oCode string) string {
	return fmt.Sprintf("ExpireOrder:%s", oCode)
}

type ExpireOrderWorkflowInput struct {
	OrderCode string
}

// ExpireOrder times out an order nobody paid for, once its hold has expired.
//
//	still PENDING -> TIMEOUT; release the hold, free the purchase slot and the chair
//	anything else -> nothing: paid, failed or cancelled first
//
// Started delayed by CheckoutLifetime. See docs/design/admission-sizing.md#when-a-checkout-is-abandoned.
func ExpireOrder(ctx workflow.Context, in *ExpireOrderWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	ctx = workflow.WithActivityOptions(ctx, getCompensationActivityOptions())

	var res activities.ExpireOrderResult
	if err := executeShortStep(ctx, oActs.ExpireOrder, in.OrderCode).Get(ctx, &res); err != nil {
		return err
	}
	if !res.Expired {
		logger.Info("Order settled before its hold expired; nothing to expire", "orderCode", in.OrderCode)
		return nil
	}
	o := res.Order

	// Best-effort from here: the order is TIMEOUT, and the hold and the chair
	// each still have their own expiry behind them.
	if err := workflow.ExecuteActivity(ctx, iActs.ReleaseInventory, o.Code).Get(ctx, nil); err != nil {
		logger.Error("Failed to release a timed-out order's hold; inventory's expiry worker will", "error", err, "orderCode", o.Code)
	}
	freePurchaseSlot(ctx, o)
	if err := publishCheckoutExpired(ctx, o); err != nil {
		logger.Error("Failed to tell the waiting room; the chair is reclaimed by its TTL", "error", err, "orderCode", o.Code)
	}

	logger.Info("Order timed out unpaid", "orderCode", o.Code)
	return nil
}
