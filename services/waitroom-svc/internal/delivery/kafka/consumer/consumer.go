package consumer

import (
	"context"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

// maxRejoinBackoff caps the delay between consumer-group rejoin attempts.
const maxRejoinBackoff = 30 * time.Second

// RetryPolicy bounds how long a transient failure is retried in place before the
// message is parked. Delays grow exponentially from BaseDelay.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

func (p RetryPolicy) delayFor(attempt int) time.Duration {
	delay := p.BaseDelay
	for range attempt - 1 {
		delay *= 2
	}
	return delay
}

// messageProcessor dispatches a message to its handler. It is a field rather
// than a direct call so delivery semantics can be tested without Kafka.
type messageProcessor func(ctx context.Context, msg *sarama.ConsumerMessage) error

type Consumer struct {
	consGr  sarama.ConsumerGroup
	wrSvc   service.WaitroomService
	dlq     producer.DeadLetterPublisher
	policy  RetryPolicy
	process messageProcessor
	l       logger.Logger
	wg      sync.WaitGroup
}

func NewConsumer(
	consGr sarama.ConsumerGroup,
	wrSvc service.WaitroomService,
	dlq producer.DeadLetterPublisher,
	policy RetryPolicy,
	l logger.Logger,
) *Consumer {
	c := &Consumer{
		consGr: consGr,
		wrSvc:  wrSvc,
		dlq:    dlq,
		policy: policy,
		l:      l,
	}
	c.process = c.processMessage

	return c
}

func (c *Consumer) processMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	switch msg.Topic {
	case kafka.TopicCheckoutCompleted:
		return c.HandleCheckoutCompleted(ctx, msg)
	case kafka.TopicCheckoutFailed:
		return c.HandleCheckoutFailed(ctx, msg)
	case kafka.TopicCheckoutExpired:
		return c.HandleCheckoutExpired(ctx, msg)
	default:
		c.l.Warnf(ctx, "Unknown topic, skipping - topic: %s", msg.Topic)
		return nil
	}
}

// deliver runs a message to a terminal outcome and reports whether its offset
// may be committed.
//
// A committed offset must mean "this message will never need to be seen again".
// Marking an offset the handler failed on loses the message for good: sarama's
// offset manager keeps the highest mark, so the next success commits straight
// past the failure and nothing ever redelivers it.
//
// Returning is a poor retry channel. Sarama cancels the whole session as soon
// as any ConsumeClaim returns (`defer sess.cancel()`, consumer_group.go:871),
// so every transient blip would cost a full consumer-group rebalance across all
// partitions, and Start's re-Consume loop would spin on it. So a message is
// retried here, in place, and parked in the dead-letter topic if it cannot be
// made to succeed.
func (c *Consumer) deliver(ctx context.Context, msg *sarama.ConsumerMessage) (commit bool, err error) {
	maxAttempts := max(c.policy.MaxAttempts, 1)

	var lastErr error
	attempts := 0

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attempts = attempt

		lastErr = c.process(ctx, msg)
		if lastErr == nil {
			return true, nil
		}

		// A payload the handler cannot interpret will fail identically every
		// time; retrying only delays the rest of the partition.
		if isPermanent(lastErr) {
			break
		}

		if attempt == maxAttempts {
			break
		}

		c.l.Warnf(ctx, "Message handling failed, retrying - topic: %s, partition: %d, offset: %d, attempt: %d/%d, error: %v",
			msg.Topic, msg.Partition, msg.Offset, attempt, maxAttempts, lastErr)

		select {
		case <-time.After(c.policy.delayFor(attempt)):
		case <-ctx.Done():
			// Shutting down or rebalancing. Leave the offset uncommitted so the
			// message is redelivered to whoever picks up this partition next.
			return false, nil
		}
	}

	c.l.Errorf(ctx, "Message exhausted handling, routing to DLQ - topic: %s, partition: %d, offset: %d, attempts: %d, error: %v",
		msg.Topic, msg.Partition, msg.Offset, attempts, lastErr)

	if dlqErr := c.dlq.PublishDeadLetter(ctx, msg, lastErr.Error(), attempts); dlqErr != nil {
		// Nowhere safe to put it. Returning ends the session, so the message is
		// redelivered after the rejoin (which backs off) and we try again --
		// committing would drop it outright.
		return false, dlqErr
	}

	return true, nil
}

func (c *Consumer) Start(ctx context.Context) {
	topics := []string{kafka.TopicCheckoutCompleted, kafka.TopicCheckoutFailed, kafka.TopicCheckoutExpired}
	c.wg.Go(func() {
		// Consume returns on every rebalance, and on any ConsumeClaim exit.
		// Rejoining immediately turns a persistent failure (broker down, DLQ
		// unwritable) into a rebalance storm, so back off between attempts.
		backoff := c.policy.BaseDelay

		for {
			if err := c.consGr.Consume(ctx, topics, c); err != nil {
				c.l.Errorf(ctx, "delivery.kafka.consumer.consumer.Start: %v", err)

				select {
				case <-time.After(backoff):
					backoff = min(backoff*2, maxRejoinBackoff)
				case <-ctx.Done():
				}
			} else {
				backoff = c.policy.BaseDelay
			}

			if ctx.Err() != nil {
				c.l.Infof(ctx, "delivery.kafka.consumer.consumer.Start: %v", ctx.Err())
				return
			}
		}
	})

	// Handle errors
	c.wg.Go(func() {
		for err := range c.consGr.Errors() {
			c.l.Errorf(ctx, "delivery.kafka.consumer.consumer.Start: %v", err)
		}
	})

	c.l.Infof(ctx, "Consumer is consuming topics: %v", topics)
}

func (c *Consumer) Close() error {
	if err := c.consGr.Close(); err != nil {
		return err
	}

	c.wg.Wait()
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
		case message, ok := <-claim.Messages():
			if !ok {
				return nil
			}

			commit, err := c.deliver(ss.Context(), message)
			if err != nil {
				return err
			}

			if !commit {
				// The offset was deliberately left uncommitted. Stop consuming
				// this claim so no later message can commit past it -- sarama
				// keeps the highest mark, so continuing here would lose it.
				return nil
			}

			ss.MarkMessage(message, "")

		case <-ss.Context().Done():
			return nil
		}
	}
}
