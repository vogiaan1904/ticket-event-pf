package workflows

import (
	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"go.temporal.io/sdk/workflow"
)

// executeShortStep runs a short, idempotent step inside the workflow worker as a
// local activity, keeping the context's retry policy. Sagas that started before
// the change replay on the regular path. See docs/decisions/0018.
func executeShortStep(ctx workflow.Context, activity interface{}, args ...interface{}) workflow.Future {
	if workflow.GetVersion(ctx, shortStepsChangeID, workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return workflow.ExecuteActivity(ctx, activity, args...)
	}

	ao := workflow.GetActivityOptions(ctx)
	ao.StartToCloseTimeout = shortStepAttemptTimeout
	lctx := workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{
		StartToCloseTimeout:    shortStepAttemptTimeout,
		ScheduleToCloseTimeout: activityRetryBudget(ao),
		RetryPolicy:            ao.RetryPolicy,
	})
	return workflow.ExecuteLocalActivity(lctx, activity, args...)
}

// validateOrder loads the order an event is about, passing the failure through
// as it arrived. GetOrder already tags a genuinely missing order; relabelling
// every failure that way would hide an unreachable datastore.
func validateOrder(ctx workflow.Context, code string) (*models.Order, error) {
	var ord *models.Order
	if err := executeShortStep(ctx, oActs.GetOrder, code).Get(ctx, &ord); err != nil {
		return nil, err
	}

	return ord, nil
}

func createOrder(ctx workflow.Context, in *CreateOrderWorkflowInput) (*models.Order, error) {
	opt := repo.CreateOrderOption{
		SessionID:    in.SessionID,
		Code:         in.OrderCode,
		UserID:       in.UserID,
		Email:        in.Email,
		Phone:        in.Phone,
		UserFullName: in.UserFullName,
		EventID:      in.EventID,
		Currency:     in.Currency,
		Status:       models.OrderStatusPending,
		TotalAmount:  in.TotalAmount,
	}

	var o *models.Order
	err := executeShortStep(ctx, oActs.CreateOrder, opt).Get(ctx, &o)
	return o, err
}

func createOrderItems(ctx workflow.Context, code string, ins []CreateOrderItemInput) ([]models.OrderItem, error) {
	opts := make([]repo.CreateOrderItemOption, len(ins))
	for i, itm := range ins {
		opts[i] = repo.CreateOrderItemOption{
			OrderCode:       code,
			TicketClassID:   itm.TicketClassID,
			TicketClassName: itm.TicketClassName,
		}
	}
	var itms []models.OrderItem

	err := executeShortStep(ctx, oActs.CreateOrderItems, code, opts).Get(ctx, &itms)
	return itms, err
}

func reserveInventory(ctx workflow.Context, code string, expAt string, ins []CreateOrderItemInput) error {
	rsvItms := make([]*inventory.ReserveItem, len(ins))
	for i, itm := range ins {
		rsvItms[i] = &inventory.ReserveItem{
			TicketClassId: itm.TicketClassID,
			Quantity:      itm.Quantity,
		}
	}

	err := workflow.ExecuteActivity(ctx, iActs.ReserveInventory, code, expAt, rsvItms).Get(ctx, nil)
	return err
}

func updateOrderStatus(ctx workflow.Context, code string, status models.OrderStatus) error {
	err := executeShortStep(ctx, oActs.UpdateOrderStatus, code, status).Get(ctx, nil)
	return err
}

func processPayment(ctx workflow.Context, in *CreateOrderWorkflowInput) (*payment.CreatePaymentIntentResponse, error) {
	var resp *payment.CreatePaymentIntentResponse
	err := workflow.ExecuteActivity(ctx, pActs.CreatePaymentIntent,
		activities.CreatePaymentIntentInput{
			OrderCode:      in.OrderCode,
			TotalAmount:    in.TotalAmount,
			Currency:       in.Currency,
			Provider:       in.PaymentProvider,
			RedirectUrl:    in.RedirectUrl,
			IdempotencyKey: in.IdempotencyKey,
			TimeoutSeconds: int32(PaymentTimeout.Seconds()),
		},
	).Get(ctx, &resp)
	return resp, err
}

func confirmInventory(ctx workflow.Context, code string) error {
	err := workflow.ExecuteActivity(ctx, iActs.ConfirmInventory, code).Get(ctx, nil)
	return err
}

func releasePurchaseSlot(ctx workflow.Context, o *models.Order) error {
	key := order.PurchaseSlotKey(o.SessionID, o.UserID, o.EventID)

	return executeShortStep(ctx, oActs.ReleasePurchaseSlot, key, o.Code).Get(ctx, nil)
}

func publishCheckoutCompleted(ctx workflow.Context, ssID, userID, eventID string) error {
	if ssID == "" {
		return nil
	}

	err := executeShortStep(ctx, epActs.PublishCheckoutCompleted,
		activities.PublishCheckoutCompletedInput{
			SessionID: ssID,
			UserID:    userID,
			EventID:   eventID,
		}).Get(ctx, nil)
	return err
}

func publishCheckoutExpired(ctx workflow.Context, o *models.Order) error {
	if o.SessionID == "" {
		return nil
	}

	return executeShortStep(ctx, epActs.PublishCheckoutExpired,
		activities.PublishCheckoutExpiredInput{
			SessionID: o.SessionID,
			UserID:    o.UserID,
			EventID:   o.EventID,
		}).Get(ctx, nil)
}

func publishRefundRequired(ctx workflow.Context, o *models.Order, reason string) error {
	return workflow.ExecuteActivity(ctx, epActs.PublishRefundRequired,
		activities.PublishRefundRequiredInput{
			OrderCode: o.Code,
			UserID:    o.UserID,
			EventID:   o.EventID,
			Reason:    reason,
		}).Get(ctx, nil)
}
