package kafka

import (
	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-order/config"
	pkgKafka "github.com/vogiaan1904/ticketbottle-order/pkg/kafka"
)

func NewConsumerGroup(cfg config.KafkaConfig) (sarama.ConsumerGroup, error) {
	consGr, err := pkgKafka.NewConsumer(pkgKafka.ConsumerConfig{
		Brokers: cfg.Brokers,
		GroupID: cfg.ConsumerGroupID,
	})
	if err != nil {
		return nil, err
	}

	return consGr, nil
}
