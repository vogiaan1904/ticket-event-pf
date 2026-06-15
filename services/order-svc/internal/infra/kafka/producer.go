package kafka

import (
	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-order/config"
	pkgKafka "github.com/vogiaan1904/ticketbottle-order/pkg/kafka"
)

func NewProducer(cfg config.KafkaConfig) (sarama.SyncProducer, error) {
	prod, err := pkgKafka.NewProducer(pkgKafka.ProducerConfig{
		Brokers:      cfg.Brokers,
		RetryMax:     cfg.ProducerRetryMax,
		RequiredAcks: cfg.ProducerRequiredAcks,
	})
	if err != nil {
		return nil, err
	}

	return prod, nil
}
