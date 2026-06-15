package activities

import (
	"context"

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

	return a.Prod.PublishCheckoutCompleted(ctx, event)
}
