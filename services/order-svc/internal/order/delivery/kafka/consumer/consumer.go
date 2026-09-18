package consumer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
)

// redeliveryBackoff paces a message that keeps failing. Ending the session puts
// it back and the next session re-reads the same offset immediately, so without
// the pause a permanently failing message spins its partition through rebalances.
const redeliveryBackoff = 5 * time.Second

type Consumer struct {
	consGr sarama.ConsumerGroup
	svc    order.Service
	l      logger.Logger
	wg     sync.WaitGroup

	backoff time.Duration
}

func NewConsumer(
	consGr sarama.ConsumerGroup,
	svc order.Service,
	l logger.Logger,
) *Consumer {
	return &Consumer{
		consGr:  consGr,
		svc:     svc,
		l:       l,
		backoff: redeliveryBackoff,
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

// isSettled reports whether a failed message already has its final answer, so
// another delivery could only reach the same one: the order's state accounts for
// this payment, or there is no order to account for it. Everything else -- an
// unreachable dependency, a deadline, a bug -- may succeed later.
func isSettled(err error) bool {
	return errors.Is(err, order.ErrOrderAlreadyProcessed) || errors.Is(err, order.ErrOrderNotFound)
}

// ConsumeClaim processes a partition's messages in order, marking each only once
// handled. A failure is left unmarked and ends the session -- sarama commits the
// highest marked offset, so moving on would drop the event for good. A settled
// failure is marked instead; redelivering it re-derives the same answer forever
// and holds every later event on the partition behind it.
func (c *Consumer) ConsumeClaim(ss sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case message := <-claim.Messages():
			if message == nil {
				return nil
			}

			err := c.processMessage(ss.Context(), message)
			if err == nil {
				ss.MarkMessage(message, "")
				continue
			}

			if isSettled(err) {
				c.l.Warnf(ss.Context(), "delivery.kafka.consumer.consumer.ConsumeClaim: %s offset %d has been answered and will not be redelivered: %v", message.Topic, message.Offset, err)
				ss.MarkMessage(message, "")
				continue
			}

			c.l.Errorf(ss.Context(), "delivery.kafka.consumer.consumer.ConsumeClaim: %s offset %d failed and will be redelivered: %v", message.Topic, message.Offset, err)

			select {
			case <-time.After(c.backoff):
			case <-ss.Context().Done():
			}

			return err

		case <-ss.Context().Done():
			return nil
		}
	}
}
