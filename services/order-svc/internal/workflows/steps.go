package workflows

import (
	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func validateOrder(ctx workflow.Context, code string) (*models.Order, error) {
	var ord *models.Order
	err := workflow.ExecuteActivity(ctx, oActs.GetOrder, code).Get(ctx, &ord)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"Failed to get order",
			"ORDER_NOT_FOUND",
			err,
		)
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
	err := workflow.ExecuteActivity(ctx, oActs.CreateOrder, opt).Get(ctx, &o)
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

	err := workflow.ExecuteActivity(ctx, oActs.CreateOrderItems, code, opts).Get(ctx, &itms)
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
	err := workflow.ExecuteActivity(ctx, oActs.UpdateOrderStatus, code, status).Get(ctx, nil)
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

	return workflow.ExecuteActivity(ctx, oActs.ReleasePurchaseSlot, key, o.Code).Get(ctx, nil)
}

func publishCheckoutCompleted(ctx workflow.Context, ssID, userID, eventID string) error {
	if ssID == "" {
		return nil
	}

	err := workflow.ExecuteActivity(ctx, epActs.PublishCheckoutCompleted,
		activities.PublishCheckoutCompletedInput{
			SessionID: ssID,
			UserID:    userID,
			EventID:   eventID,
		}).Get(ctx, nil)
	return err
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
