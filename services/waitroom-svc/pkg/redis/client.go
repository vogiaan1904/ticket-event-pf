package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vogiaan1904/ticketbottle-waitroom/config"
)

// Nil is a sentinel error returned when a key does not exist
var Nil = redis.Nil

// Z represents a sorted set member with score
type Z = redis.Z

// PubSub represents a Redis Pub/Sub subscription
type PubSub = redis.PubSub

// Script represents a Lua script
type Script = redis.Script

type Client struct {
	redis *redis.Client
}

func NewClient(cfg config.RedisConfig) *Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		MaxRetries:   cfg.MaxRetries,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
	})

	return &Client{
		redis: rdb,
	}
}

// Get retrieves the value of the specified key from Redis.
// Returns the value as bytes and any error encountered.
func (r *Client) Get(ctx context.Context, key string) ([]byte, error) {
	return r.redis.Get(ctx, key).Bytes()
}

// GetString retrieves the value of the specified key from Redis as a string.
// Returns the value as a string and any error encountered.
func (r *Client) GetString(ctx context.Context, key string) (string, error) {
	return r.redis.Get(ctx, key).Result()
}

// Set stores a key-value pair in Redis with an optional expiration time.
// If expiration is 0, the key will have no expiration.
// Returns an error if the operation fails.
func (r *Client) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	return r.redis.Set(ctx, key, value, expiration).Err()
}

// Del deletes the specified key from Redis.
// Returns an error if the operation fails.
func (r *Client) Del(ctx context.Context, key string) error {
	return r.redis.Del(ctx, key).Err()
}

// Exists checks whether a given key exists in Redis.
// Returns the count of existing keys and any error encountered.
func (r *Client) Exists(ctx context.Context, key string) (int64, error) {
	return r.redis.Exists(ctx, key).Result()
}

// TTL returns the remaining time to live of a key that has a timeout.
// Returns the TTL duration and any error encountered.
func (r *Client) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.redis.TTL(ctx, key).Result()
}

// Pipeline creates a pipeline for executing multiple commands atomically.
// Returns a Pipeliner that can be used to queue commands and execute them together.
func (r *Client) Pipeline() redis.Pipeliner {
	return r.redis.Pipeline()
}

// HSet sets a field in a hash stored at key in Redis.
// Returns an error if the operation fails.
func (r *Client) HSet(ctx context.Context, key string, value any) error {
	return r.redis.HSet(ctx, key, value).Err()
}

// HGetAll retrieves all fields and values of a hash stored at the specified key.
// The results are scanned into the provided destination structure.
// Returns an error if the operation fails.
func (r *Client) HGetAll(ctx context.Context, key string, desc any) error {
	return r.redis.HGetAll(ctx, key).Scan(desc)
}

// Ping sends a PING command to Redis to check connectivity.
// Returns an error if Redis is not reachable.
func (r *Client) Ping(ctx context.Context) error {
	return r.redis.Ping(ctx).Err()
}

// Close gracefully shuts down the Redis client connection.
func (r *Client) Close() {
	r.redis.Close()
}

// GetClient returns the underlying Redis client for advanced operations
func (r *Client) GetClient() *redis.Client {
	return r.redis
}

// ============= Sorted Set Operations =============

// ZAdd adds one or more members to a sorted set, or updates the score if it already exists.
func (r *Client) ZAdd(ctx context.Context, key string, members ...Z) error {
	return r.redis.ZAdd(ctx, key, members...).Err()
}

// ZRem removes one or more members from a sorted set.
func (r *Client) ZRem(ctx context.Context, key string, members ...any) (int64, error) {
	return r.redis.ZRem(ctx, key, members...).Result()
}

// ZCard returns the number of members in a sorted set.
func (r *Client) ZCard(ctx context.Context, key string) (int64, error) {
	return r.redis.ZCard(ctx, key).Result()
}

// ZRank returns the rank of a member in a sorted set (0-indexed, lowest score first).
func (r *Client) ZRank(ctx context.Context, key, member string) (int64, error) {
	return r.redis.ZRank(ctx, key, member).Result()
}

// ZRange returns a range of members in a sorted set by index.
func (r *Client) ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return r.redis.ZRange(ctx, key, start, stop).Result()
}

// ============= Set Operations =============

// SAdd adds one or more members to a set.
func (r *Client) SAdd(ctx context.Context, key string, members ...any) error {
	return r.redis.SAdd(ctx, key, members...).Err()
}

// SRem removes one or more members from a set.
func (r *Client) SRem(ctx context.Context, key string, members ...any) error {
	return r.redis.SRem(ctx, key, members...).Err()
}

// SCard returns the number of members in a set.
func (r *Client) SCard(ctx context.Context, key string) (int64, error) {
	return r.redis.SCard(ctx, key).Result()
}

// SIsMember checks if a value is a member of a set.
func (r *Client) SIsMember(ctx context.Context, key string, member any) (bool, error) {
	return r.redis.SIsMember(ctx, key, member).Result()
}

// SMembers returns all members of a set.
func (r *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	return r.redis.SMembers(ctx, key).Result()
}

// ============= Pub/Sub Operations =============

// Publish posts a message to a channel.
func (r *Client) Publish(ctx context.Context, channel string, message any) error {
	return r.redis.Publish(ctx, channel, message).Err()
}

// Subscribe subscribes to one or more channels.
func (r *Client) Subscribe(ctx context.Context, channels ...string) *PubSub {
	return r.redis.Subscribe(ctx, channels...)
}

// ============= Utility Operations =============

// Expire sets a timeout on a key.
func (r *Client) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return r.redis.Expire(ctx, key, expiration).Err()
}

// NewScript creates a new Lua script.
func NewScript(src string) *Script {
	return redis.NewScript(src)
}
