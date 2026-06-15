package producer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/IBM/sarama"
	kafka "github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-order/pkg/util"
)

type Producer interface {
	PublishCheckoutCompleted(ctx context.Context, event kafka.CheckoutCompletedEvent) error
	PublishCheckoutFailed(ctx context.Context, event kafka.CheckoutFailedEvent) error

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

func (p implProducer) PublishCheckoutCompleted(ctx context.Context, event kafka.CheckoutCompletedEvent) error {
	event.Timestamp = util.TimeToISO8601Str(time.Now())
	val, err := json.Marshal(event)
	if err != nil {
		p.l.Errorf(ctx, "order.delivery.kafka.producer.PublishCheckoutCompleted: %v", err)
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic: kafka.TopicCheckoutCompleted,
		Key:   sarama.StringEncoder(event.EventID),
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

func (p *implProducer) PublishCheckoutFailed(ctx context.Context, event kafka.CheckoutFailedEvent) error {
	event.Timestamp = time.Now().String()
	val, err := json.Marshal(event)
	if err != nil {
		p.l.Errorf(ctx, "order.delivery.kafka.producer.publishCheckoutFailed: %v", err)
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic: kafka.TopicCheckoutFailed,
		Key:   sarama.StringEncoder(event.EventID),
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
