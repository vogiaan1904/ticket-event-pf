package producer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/IBM/sarama"
	kafka "github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type Producer interface {
	PublishQueueReady(ctx context.Context, event kafka.QueueReadyEvent) error
	PublishQueueJoined(ctx context.Context, event kafka.QueueJoinedEvent) error
	PublishQueueLeft(ctx context.Context, event kafka.QueueLeftEvent) error
	Close() error
}

type implProducer struct {
	l    logger.Logger
	prod sarama.SyncProducer
}

func NewProducer(prod sarama.SyncProducer, l logger.Logger) Producer {
	return &implProducer{
		l:    l,
		prod: prod,
	}
}

func (p implProducer) PublishQueueReady(ctx context.Context, event kafka.QueueReadyEvent) error {
	event.Timestamp = time.Now()
	val, err := json.Marshal(event)
	if err != nil {
		p.l.Errorf(ctx, "delivery.kafka.producer.queue_producer.PublishQueueReady: %v", err)
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic: kafka.TopicQueueReady,
		Key:   sarama.StringEncoder(event.EventID), // Partition by event_id for ordering
		Value: sarama.ByteEncoder(val),
		Headers: []sarama.RecordHeader{
			{
				Key:   []byte("timestamp"),
				Value: []byte(time.Now().Format(time.RFC3339)),
			},
		},
	}

	_, _, err = p.prod.SendMessage(msg)
	return err
}

func (p *implProducer) PublishQueueJoined(ctx context.Context, event kafka.QueueJoinedEvent) error {
	event.Timestamp = time.Now()
	val, err := json.Marshal(event)
	if err != nil {
		p.l.Errorf(ctx, "delivery.kafka.producer.queue_producer.PublishQueueJoined: %v", err)
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic: kafka.TopicQueueJoined,
		Key:   sarama.StringEncoder(event.EventID), // Partition by event_id for ordering
		Value: sarama.ByteEncoder(val),
		Headers: []sarama.RecordHeader{
			{
				Key:   []byte("timestamp"),
				Value: []byte(time.Now().Format(time.RFC3339)),
			},
		},
	}

	_, _, err = p.prod.SendMessage(msg)
	return err
}

func (p *implProducer) PublishQueueLeft(ctx context.Context, event kafka.QueueLeftEvent) error {
	event.Timestamp = time.Now()
	val, err := json.Marshal(event)
	if err != nil {
		p.l.Errorf(ctx, "delivery.kafka.producer.queue_producer.PublishQueueLeft: %v", err)
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic: kafka.TopicQueueLeft,
		Key:   sarama.StringEncoder(event.EventID), // Partition by event_id for ordering
		Value: sarama.ByteEncoder(val),
		Headers: []sarama.RecordHeader{
			{
				Key:   []byte("timestamp"),
				Value: []byte(time.Now().Format(time.RFC3339)),
			},
		},
	}

	_, _, err = p.prod.SendMessage(msg)
	return err
}

func (p *implProducer) Close() error {
	if err := p.prod.Close(); err != nil {
		return err
	}

	return nil
}
