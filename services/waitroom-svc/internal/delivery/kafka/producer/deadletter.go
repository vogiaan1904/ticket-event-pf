package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	kafka "github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

// DeadLetterPublisher parks a message that could not be processed, so the
// partition it came from can keep moving without the payload being lost.
type DeadLetterPublisher interface {
	PublishDeadLetter(ctx context.Context, msg *sarama.ConsumerMessage, reason string, attempts int) error
}

type deadLetterProducer struct {
	l    logger.Logger
	prod sarama.SyncProducer
}

func NewDeadLetterProducer(prod sarama.SyncProducer, l logger.Logger) DeadLetterPublisher {
	return &deadLetterProducer{prod: prod, l: l}
}

// PublishDeadLetter republishes the original payload verbatim onto
// <topic>.dlq. The body is untouched so a replay can re-consume it as-is; the
// failure context travels in headers instead.
func (p *deadLetterProducer) PublishDeadLetter(ctx context.Context, msg *sarama.ConsumerMessage, reason string, attempts int) error {
	dlqTopic := kafka.DLQTopic(msg.Topic)

	dlqMsg := &sarama.ProducerMessage{
		Topic: dlqTopic,
		Key:   sarama.ByteEncoder(msg.Key),
		Value: sarama.ByteEncoder(msg.Value),
		Headers: []sarama.RecordHeader{
			{Key: []byte("dlq.original_topic"), Value: []byte(msg.Topic)},
			{Key: []byte("dlq.original_partition"), Value: fmt.Appendf(nil, "%d", msg.Partition)},
			{Key: []byte("dlq.original_offset"), Value: fmt.Appendf(nil, "%d", msg.Offset)},
			{Key: []byte("dlq.attempts"), Value: fmt.Appendf(nil, "%d", attempts)},
			{Key: []byte("dlq.reason"), Value: []byte(reason)},
			{Key: []byte("dlq.failed_at"), Value: []byte(time.Now().UTC().Format(time.RFC3339))},
		},
	}

	if _, _, err := p.prod.SendMessage(dlqMsg); err != nil {
		p.l.Errorf(ctx, "delivery.kafka.producer.PublishDeadLetter: topic=%s offset=%d: %v",
			msg.Topic, msg.Offset, err)
		return err
	}

	p.l.Warnf(ctx, "Message parked in dead-letter topic - topic: %s, dlq_topic: %s, partition: %d, offset: %d, attempts: %d, reason: %s",
		msg.Topic, dlqTopic, msg.Partition, msg.Offset, attempts, reason)

	return nil
}
