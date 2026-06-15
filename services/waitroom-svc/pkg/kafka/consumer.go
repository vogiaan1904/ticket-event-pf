package kafka

import (
	"fmt"
	"log"

	"github.com/IBM/sarama"
)

type ConsumerConfig struct {
	Brokers []string
	GroupID string
}

func NewConsumer(cfg ConsumerConfig) (sarama.ConsumerGroup, error) {
	saramaCfg := sarama.NewConfig()
	saramaCfg.Version = sarama.V2_8_0_0
	saramaCfg.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
	saramaCfg.Consumer.Offsets.Initial = sarama.OffsetNewest
	saramaCfg.Consumer.Return.Errors = true

	consGroup, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.GroupID, saramaCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka consumer group: %w", err)
	}

	log.Printf("Kafka consumer connected to brokers: %v, group: %s\n", cfg.Brokers, cfg.GroupID)

	return consGroup, nil
}
