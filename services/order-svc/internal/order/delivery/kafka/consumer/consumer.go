package consumer

import (
	"context"
	"sync"

	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
)

type Consumer struct {
	consGr sarama.ConsumerGroup
	svc    order.Service
	l      logger.Logger
	wg     sync.WaitGroup
}

func NewConsumer(
	consGr sarama.ConsumerGroup,
	svc order.Service,
	l logger.Logger,
) *Consumer {
	return &Consumer{
		consGr: consGr,
		svc:    svc,
		l:      l,
	}
}

func (c *Consumer) processMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	switch msg.Topic {
	case kafka.TopicPaymentCompleted:
		return c.HandlePaymentCompleted(ctx, msg)
	case kafka.TopicPaymentFailed:
		return c.HandlePaymentFailed(ctx, msg)
	default:
		c.l.Warn(ctx, "Unknown topic", "topic", msg.Topic)
		return nil
	}
}

func (c *Consumer) Start(ctx context.Context) error {
	topics := []string{kafka.TopicPaymentCompleted, kafka.TopicPaymentFailed}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			// `Consume` should be called inside an infinite loop, when a
			// server-side rebalance happens, the consumer session will need to be
			// recreated to get the new claims
			if err := c.consGr.Consume(ctx, topics, c); err != nil {
				c.l.Errorf(ctx, "delivery.kafka.consumer.consumer.Start: %v", err)
			}

			// check if context was cancelled, signaling that the consumer should stop
			if ctx.Err() != nil {
				c.l.Infof(ctx, "Context cancelled, stopping consumer")
				return
			}
		}
	}()

	c.l.Infof(ctx, "Consumer is consuming topics: %v", topics)
	return nil
}

func (c *Consumer) Close() error {
	// Wait for all goroutines to finish first
	c.wg.Wait()

	// Then close the consumer group
	if err := c.consGr.Close(); err != nil {
		return err
	}

	return nil
}

func (c *Consumer) Setup(sarama.ConsumerGroupSession) error {
	c.l.Debug(context.Background(), "Consumer group session started")
	return nil
}

func (c *Consumer) Cleanup(sarama.ConsumerGroupSession) error {
	c.l.Debug(context.Background(), "Consumer group session ended")
	return nil
}

func (c *Consumer) ConsumeClaim(ss sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case message := <-claim.Messages():
			if message == nil {
				return nil
			}

			if err := c.processMessage(ss.Context(), message); err != nil {
				c.l.Error(ss.Context(), "delivery.kafka.consumer.consumer.ConsumeClaim: %v", err,
					"topic", message.Topic,
					"offset", message.Offset,
				)
				continue
			}

			ss.MarkMessage(message, "")

		case <-ss.Context().Done():
			return nil
		}
	}
}
