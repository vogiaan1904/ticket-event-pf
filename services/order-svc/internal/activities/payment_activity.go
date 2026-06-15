package activities

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
)

type PaymentActivities struct {
	Client payment.PaymentServiceClient
}

type CreatePaymentIntentInput struct {
	OrderCode      string
	TotalAmount    int64
	Currency       string
	Provider       string
	RedirectUrl    string
	IdempotencyKey string
	TimeoutSeconds int32
}

func NewPaymentActivities(client payment.PaymentServiceClient) *PaymentActivities {
	return &PaymentActivities{
		Client: client,
	}
}

func (a *PaymentActivities) CreatePaymentIntent(ctx context.Context, in *CreatePaymentIntentInput) (*payment.CreatePaymentIntentResponse, error) {
	resp, err := a.Client.CreatePaymentIntent(ctx, &payment.CreatePaymentIntentRequest{
		OrderCode:      in.OrderCode,
		AmountCents:    in.TotalAmount,
		Currency:       in.Currency,
		Provider:       payment.PaymentProvider(payment.PaymentProvider_value[in.Provider]),
		RedirectUrl:    in.RedirectUrl,
		IdempotencyKey: in.IdempotencyKey,
		TimeoutSeconds: in.TimeoutSeconds,
	})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// CancelPayment cancels a payment (if supported by payment service)
// Note: This method may need to be implemented based on your payment service interface
func (a *PaymentActivities) CancelPayment(ctx context.Context, orderCode string, reason string) error {
	// TODO: Implement if payment service supports cancellation
	// For now, we'll just log the cancellation request
	return nil
}
