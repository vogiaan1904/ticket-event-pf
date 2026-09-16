package activities

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-order/internal/metrics"
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
		metrics.RecordActivityFailure("CreatePaymentIntent", err)
		return nil, err
	}

	return resp, nil
}

// CancelPayment is an unused stub: no saga step registers it, so a failed
// create leaves the intent to expire on payment-svc's own timeout.
// TODO: wire to payment.CancelPaymentIntent before any caller depends on it.
func (a *PaymentActivities) CancelPayment(ctx context.Context, orderCode string, reason string) error {
	return nil
}
