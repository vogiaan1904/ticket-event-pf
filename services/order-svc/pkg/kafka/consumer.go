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

// newConsumerConfig reads a partition with no committed offset from its oldest
// message. A partition added to a live topic is written to before the group claims
// it, so starting at the newest would skip payments (decision 0030).
func newConsumerConfig() *sarama.Config {
	c := sarama.NewConfig()
	c.Version = sarama.V2_8_0_0
	c.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
	c.Consumer.Offsets.Initial = sarama.OffsetOldest
	c.Consumer.Return.Errors = true
	return c
}

func NewConsumer(cfg ConsumerConfig) (sarama.ConsumerGroup, error) {
	consGroup, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.GroupID, newConsumerConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka consumer group: %w", err)
	}

	log.Printf("Kafka consumer connected to brokers: %v, group: %s\n", cfg.Brokers, cfg.GroupID)

	return consGroup, nil
}
