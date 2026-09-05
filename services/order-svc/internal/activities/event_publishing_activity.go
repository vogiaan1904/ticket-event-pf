package activities

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-order/internal/metrics"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka/producer"
)

type EventPublishingActivities struct {
	Prod producer.Producer
}

func NewEventPublishingActivities(prod producer.Producer) *EventPublishingActivities {
	return &EventPublishingActivities{
		Prod: prod,
	}
}

type PublishCheckoutCompletedInput struct {
	SessionID string
	UserID    string
	EventID   string
}

func (a *EventPublishingActivities) PublishCheckoutCompleted(ctx context.Context, in PublishCheckoutCompletedInput) error {
	if in.SessionID == "" {
		return nil
	}

	event := kafka.CheckoutCompletedEvent{
		SessionID: in.SessionID,
		UserID:    in.UserID,
		EventID:   in.EventID,
	}

	err := a.Prod.PublishCheckoutCompleted(ctx, event)
	metrics.RecordActivityFailure("PublishCheckoutCompleted", err)
	return err
}

type PublishRefundRequiredInput struct {
	OrderCode string
	UserID    string
	EventID   string
	Reason    string
}

func (a *EventPublishingActivities) PublishRefundRequired(ctx context.Context, in PublishRefundRequiredInput) error {
	err := a.Prod.PublishRefundRequired(ctx, kafka.RefundRequiredEvent{
		OrderCode: in.OrderCode,
		UserID:    in.UserID,
		EventID:   in.EventID,
		Reason:    in.Reason,
	})
	metrics.RecordActivityFailure("PublishRefundRequired", err)
	return err
}
