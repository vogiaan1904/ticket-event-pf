package redis

import (
	"github.com/redis/go-redis/v9"
	"github.com/vogiaan1904/ticketbottle-order/config"
)

func NewClient(cfg config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		MaxRetries:   cfg.MaxRetries,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
	})

	return client, nil
}
