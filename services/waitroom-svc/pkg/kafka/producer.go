package kafka

import (
	"fmt"
	"log"

	"github.com/IBM/sarama"
)

type ProducerConfig struct {
	Brokers      []string
	RetryMax     int
	RequiredAcks int
}

func NewProducer(cfg ProducerConfig) (sarama.SyncProducer, error) {
	saramaCfg := sarama.NewConfig()
	saramaCfg.Producer.RequiredAcks = sarama.RequiredAcks(cfg.RequiredAcks)
	saramaCfg.Producer.Retry.Max = cfg.RetryMax
	saramaCfg.Producer.Return.Successes = true

	prod, err := sarama.NewSyncProducer(cfg.Brokers, saramaCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka producer: %w", err)
	}

	log.Printf("Kafka producer connected to brokers: %v\n", cfg.Brokers)

	return prod, nil
}
