package service

import (
	"context"
	"time"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
)

func (s *implService) publishCheckoutCompletedEvent(ctx context.Context, in order.PubCheckoutCompletedEventInput) error {
	event := kafka.CheckoutCompletedEvent{
		SessionID: in.SessionID,
		UserID:    in.UserID,
		EventID:   in.EventID,
		Timestamp: time.Now().String(),
	}

	if err := s.prod.PublishCheckoutCompleted(ctx, event); err != nil {
		s.l.Errorf(ctx, "failed to publish checkout completed event: %v", err)
		return err
	}

	return nil
}

func (s *implService) publishCheckoutFailedEvent(ctx context.Context, in order.PubCheckoutFailedEventInput) error {
	event := kafka.CheckoutFailedEvent{
		SessionID: in.SessionID,
		UserID:    in.UserID,
		EventID:   in.EventID,
		Timestamp: time.Now().String(),
	}

	if err := s.prod.PublishCheckoutFailed(ctx, event); err != nil {
		s.l.Errorf(ctx, "failed to publish checkout failed event: %v", err)
		return err
	}

	return nil
}
