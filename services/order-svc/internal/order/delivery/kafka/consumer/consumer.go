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

// redeliveryBackoff paces a message that keeps failing. Ending the session is
// what puts the message back: the next session re-reads the same offset
// immediately, so without this pause a message that fails every time would spin
// its partition through a rebalance as fast as the broker can answer.
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

// isSettled reports whether a failed message has already been given its final
// answer, so that another delivery could only reach the same one. Both cases
// are outcomes rather than faults: the order's state already accounts for this
// payment, or there is no order for it to account for. Everything else -- an
// unreachable dependency, a deadline, a bug -- may succeed on a later attempt.
func isSettled(err error) bool {
	return errors.Is(err, order.ErrOrderAlreadyProcessed) || errors.Is(err, order.ErrOrderNotFound)
}

// ConsumeClaim processes a partition's messages in order, marking each one only
// once it has been handled.
//
// A message that fails is deliberately left unmarked and ends the session:
// sarama commits the highest marked offset, so continuing past a failure would
// let the next successful message commit an offset beyond it and drop the
// event for good -- a captured payment whose order stays PENDING with nothing
// left to notice it. Ending the session leaves the committed offset before the
// failed message, and the next one re-reads it.
//
// A message whose failure is settled is marked instead: redelivering it would
// re-derive the same answer forever and hold every later event on the partition
// behind it.
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
