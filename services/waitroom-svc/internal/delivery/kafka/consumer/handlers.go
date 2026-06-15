package consumer

import (
	"context"
	"encoding/json"

	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
)

func (c *Consumer) HandleCheckoutCompleted(ctx context.Context, message *sarama.ConsumerMessage) error {
	c.l.Info(ctx, "HandleCheckoutCompleted consumed")

	var e kafka.CheckoutCompletedEvent
	if err := json.Unmarshal(message.Value, &e); err != nil {
		c.l.Error(ctx, "delivery.kafka.consumer.handlers.HandleCheckoutCompleted: %v", err)
		return err
	}

	if err := c.wrSvc.HandleCheckoutCompleted(ctx, service.CheckoutCompletedInput{
		SessionID: e.SessionID,
		UserID:    e.UserID,
		EventID:   e.EventID,
		Timestamp: e.Timestamp,
	}); err != nil {
		c.l.Error(ctx, "delivery.kafka.consumer.handlers.HandleCheckoutCompleted: %v", err)
		return err
	}

	return nil
}

func (c *Consumer) HandleCheckoutFailed(ctx context.Context, message *sarama.ConsumerMessage) error {
	c.l.Info(ctx, "HandleCheckoutFailed consumed")

	var e kafka.CheckoutFailedEvent
	if err := json.Unmarshal(message.Value, &e); err != nil {
		c.l.Error(ctx, "delivery.kafka.consumer.handlers.HandleCheckoutFailed: %v", err)
		return err
	}

	if err := c.wrSvc.HandleCheckoutFailed(ctx, service.CheckoutFailedInput{
		SessionID: e.SessionID,
		UserID:    e.UserID,
		EventID:   e.EventID,
		Timestamp: e.Timestamp,
	}); err != nil {
		c.l.Error(ctx, "delivery.kafka.consumer.handlers.HandleCheckoutFailed: %v", err)
		return err
	}

	return nil
}

func (c *Consumer) HandleCheckoutExpired(ctx context.Context, message *sarama.ConsumerMessage) error {
	c.l.Info(ctx, "HandleCheckoutExpired consumed")

	var e kafka.CheckoutExpiredEvent
	if err := json.Unmarshal(message.Value, &e); err != nil {
		c.l.Error(ctx, "delivery.kafka.consumer.handlers.HandleCheckoutExpired: %v", err)
		return err
	}

	if err := c.wrSvc.HandleCheckoutExpired(ctx, service.CheckoutExpiredInput{
		SessionID: e.SessionID,
		UserID:    e.UserID,
		EventID:   e.EventID,
		Timestamp: e.Timestamp,
	}); err != nil {
		c.l.Error(ctx, "delivery.kafka.consumer.handlers.HandleCheckoutExpired: %v", err)
		return err
	}

	return nil
}
