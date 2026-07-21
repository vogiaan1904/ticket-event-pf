package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/redis"
)

type QueueRepository interface {
	AddToQueue(ctx context.Context, eID string, ss *models.Session) error
	RemoveFromQueue(ctx context.Context, eID string, ssIDs ...string) error
	GetQueueLength(ctx context.Context, eID string) (int64, error)
	GetQueuePosition(ctx context.Context, eID, ssID string) (int64, error)
	GetQueueMembers(ctx context.Context, eID string, start, stop int64) ([]string, error)
	AddToProcessing(ctx context.Context, eID, ssID string, ttl time.Duration) error
	RemoveFromProcessing(ctx context.Context, eID, ssID string) error
	GetProcessingCount(ctx context.Context, eID string) (int64, error)
	IsProcessing(ctx context.Context, eID, ssID string) (bool, error)
	// Pub/Sub methods for real-time position updates
	PublishPositionUpdate(ctx context.Context, update *models.PositionUpdateEvent) error
	SubscribeToPositionUpdates(ctx context.Context, eID string) (*redis.PubSub, error)
	// Buffered QUEUE_READY publishes awaiting retry
	BufferQueueReady(ctx context.Context, payload []byte) error
	PeekBufferedQueueReady(ctx context.Context, count int) ([]string, error)
	TrimBufferedQueueReady(ctx context.Context, count int) error
	// Active events tracking
	AddActiveEvent(ctx context.Context, eID string) error
	RemoveActiveEvent(ctx context.Context, eID string) error
	GetActiveEvents(ctx context.Context) ([]string, error)
	IsActiveEvent(ctx context.Context, eID string) (bool, error)
}

type redisQueueRepository struct {
	cli *redis.Client
	l   logger.Logger
}

func NewRedisQueueRepository(cli *redis.Client, l logger.Logger) QueueRepository {
	return &redisQueueRepository{
		cli: cli,
		l:   l,
	}
}

func (r *redisQueueRepository) AddToQueue(ctx context.Context, eID string, ss *models.Session) error {
	qKey := r.queueKey(eID)
	score := ss.GetQueueScore()

	if err := r.cli.ZAdd(ctx, qKey, redis.Z{
		Score:  score,
		Member: ss.ID,
	}); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.AddToQueue: %v", err)
		return err
	}

	return nil
}

func (r *redisQueueRepository) RemoveFromQueue(ctx context.Context, eID string, ssIDs ...string) error {
	if len(ssIDs) == 0 {
		return nil
	}

	qKey := r.queueKey(eID)

	members := make([]any, len(ssIDs))
	for i, ssID := range ssIDs {
		members[i] = ssID
	}

	if _, err := r.cli.ZRem(ctx, qKey, members...); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.RemoveFromQueue: %v", err)
		return err
	}

	return nil
}

func (r *redisQueueRepository) GetQueueLength(ctx context.Context, eID string) (int64, error) {
	qKey := r.queueKey(eID)

	count, err := r.cli.ZCard(ctx, qKey)
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.GetQueueLength: %v", err)
		return 0, err
	}

	return count, nil
}

func (r *redisQueueRepository) GetQueuePosition(ctx context.Context, eID, ssID string) (int64, error) {
	qKey := r.queueKey(eID)

	rank, err := r.cli.ZRank(ctx, qKey, ssID)
	if err != nil {
		if err == redis.Nil {
			return -1, nil
		}

		r.l.Errorf(ctx, "redisQueueRepository.GetQueuePosition: %v", err)
		return 0, err
	}

	return rank + 1, nil
}

func (r *redisQueueRepository) GetQueueMembers(ctx context.Context, eID string, start, stop int64) ([]string, error) {
	qKey := r.queueKey(eID)

	mems, err := r.cli.ZRange(ctx, qKey, start, stop)
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.GetQueueMembers: %v", err)
		return nil, err
	}

	return mems, nil
}

// processingKeyGrace keeps the processing key alive past its last slot expiry so
// an event that goes quiet still gets garbage collected. Correctness comes from
// the per-member scores, never from this TTL.
const processingKeyGrace = time.Hour

// reapAndCountScript drops every slot whose expiry has passed, then counts what
// is left. Reaping and counting must be one round trip: a count taken against
// un-reaped data over-reports occupancy and starves admission.
var reapAndCountScript = redis.NewScript(`
	local key = KEYS[1]
	local now = tonumber(ARGV[1])

	redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
	return redis.call('ZCARD', key)
`)

func (r *redisQueueRepository) AddToProcessing(ctx context.Context, eID, ssID string, ttl time.Duration) error {
	pKey := r.processingKey(eID)
	expiresAt := time.Now().Add(ttl)

	pipe := r.cli.GetClient().Pipeline()
	pipe.ZAdd(ctx, pKey, redis.Z{Score: float64(expiresAt.UnixMilli()), Member: ssID})
	pipe.PExpire(ctx, pKey, ttl+processingKeyGrace)

	if _, err := pipe.Exec(ctx); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.AddToProcessing: %v", err)
		return err
	}

	return nil
}

func (r *redisQueueRepository) RemoveFromProcessing(ctx context.Context, eID, ssID string) error {
	pKey := r.processingKey(eID)

	if _, err := r.cli.ZRem(ctx, pKey, ssID); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.RemoveFromProcessing: %v", err)
		return err
	}

	return nil
}

func (r *redisQueueRepository) GetProcessingCount(ctx context.Context, eID string) (int64, error) {
	pKey := r.processingKey(eID)

	count, err := reapAndCountScript.Run(ctx, r.cli.GetClient(),
		[]string{pKey}, time.Now().UnixMilli()).Int64()
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.GetProcessingCount: %v", err)
		return 0, err
	}

	return count, nil
}

func (r *redisQueueRepository) IsProcessing(ctx context.Context, eID, ssID string) (bool, error) {
	pKey := r.processingKey(eID)

	expiresAt, err := r.cli.ZScore(ctx, pKey, ssID)
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}

		r.l.Errorf(ctx, "redisQueueRepository.IsProcessing: %v", err)
		return false, err
	}

	return int64(expiresAt) > time.Now().UnixMilli(), nil
}

func (r *redisQueueRepository) PublishPositionUpdate(ctx context.Context, update *models.PositionUpdateEvent) error {
	channel := r.positionUpdateChannel(update.EventID)

	payload, err := json.Marshal(update)
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.PublishPositionUpdate: failed to marshal update: %v", err)
		return fmt.Errorf("failed to marshal position update: %w", err)
	}

	if err := r.cli.Publish(ctx, channel, payload); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.PublishPositionUpdate: %v", err)
		return fmt.Errorf("failed to publish position update: %w", err)
	}

	return nil
}

func (r *redisQueueRepository) SubscribeToPositionUpdates(ctx context.Context, eID string) (*redis.PubSub, error) {
	channel := r.positionUpdateChannel(eID)

	pubsub := r.cli.Subscribe(ctx, channel)

	_, err := pubsub.Receive(ctx)
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.SubscribeToPositionUpdates: %v", err)
		return nil, fmt.Errorf("failed to subscribe to position updates: %w", err)
	}

	return pubsub, nil
}

func (r *redisQueueRepository) queueKey(eID string) string {
	return fmt.Sprintf("waitroom:%s:queue", eID)
}

// processingKey holds the sessions currently occupying a checkout slot, as a
// sorted set scored by absolute expiry (unix ms). Slots expire individually, so
// an abandoned checkout frees its own slot without waiting on a downstream event
// and without disturbing any other slot.
//
// Named ":checkouts" rather than ":processing" on purpose: the old key was a
// plain SET, and Redis is persistent here, so reusing the name would hit
// WRONGTYPE against a surviving key. The old key carries a TTL and expires out.
func (r *redisQueueRepository) processingKey(eID string) string {
	return fmt.Sprintf("waitroom:%s:checkouts", eID)
}

func (r *redisQueueRepository) positionUpdateChannel(eID string) string {
	return fmt.Sprintf("queue:updates:%s", eID)
}

// ============= Buffered QUEUE_READY Publishes =============

// maxBufferedQueueReady caps the retry buffer. It only grows while Kafka is
// unreachable, but an unbounded Redis list is its own outage, so the oldest
// entries are shed past this point.
const maxBufferedQueueReady = 10000

// BufferQueueReady parks a QUEUE_READY payload whose publish failed. The list is
// FIFO: appended at the tail, drained from the head, so ordering survives.
func (r *redisQueueRepository) BufferQueueReady(ctx context.Context, payload []byte) error {
	key := r.bufferedQueueReadyKey()

	length, err := r.cli.GetClient().RPush(ctx, key, payload).Result()
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.BufferQueueReady: %v", err)
		return err
	}

	if length > maxBufferedQueueReady {
		dropped := length - maxBufferedQueueReady
		r.l.Errorf(ctx, "QUEUE_READY retry buffer overflowed, dropping %d oldest event(s) - length: %d, max: %d",
			dropped, length, maxBufferedQueueReady)

		if err := r.cli.GetClient().LTrim(ctx, key, dropped, -1).Err(); err != nil {
			r.l.Errorf(ctx, "redisQueueRepository.BufferQueueReady.LTrim: %v", err)
		}
	}

	return nil
}

// PeekBufferedQueueReady reads the head of the buffer without consuming it, so a
// payload is only dropped once it has actually been published.
func (r *redisQueueRepository) PeekBufferedQueueReady(ctx context.Context, count int) ([]string, error) {
	if count <= 0 {
		return nil, nil
	}

	payloads, err := r.cli.GetClient().LRange(ctx, r.bufferedQueueReadyKey(), 0, int64(count-1)).Result()
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.PeekBufferedQueueReady: %v", err)
		return nil, err
	}

	return payloads, nil
}

// TrimBufferedQueueReady drops the first count entries -- the settled prefix.
func (r *redisQueueRepository) TrimBufferedQueueReady(ctx context.Context, count int) error {
	if count <= 0 {
		return nil
	}

	if err := r.cli.GetClient().LTrim(ctx, r.bufferedQueueReadyKey(), int64(count), -1).Err(); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.TrimBufferedQueueReady: %v", err)
		return err
	}

	return nil
}

func (r *redisQueueRepository) bufferedQueueReadyKey() string {
	return "waitroom:queue_ready:pending"
}

// ============= Active Events Tracking =============

func (r *redisQueueRepository) AddActiveEvent(ctx context.Context, eID string) error {
	activeKey := r.activeEventsKey()

	if err := r.cli.SAdd(ctx, activeKey, eID); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.AddActiveEvent: %v", err)
		return fmt.Errorf("failed to add active event: %w", err)
	}

	return nil
}

func (r *redisQueueRepository) RemoveActiveEvent(ctx context.Context, eID string) error {
	activeKey := r.activeEventsKey()

	if err := r.cli.SRem(ctx, activeKey, eID); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.RemoveActiveEvent: %v", err)
		return fmt.Errorf("failed to remove active event: %w", err)
	}

	return nil
}

func (r *redisQueueRepository) GetActiveEvents(ctx context.Context) ([]string, error) {
	activeKey := r.activeEventsKey()

	events, err := r.cli.SMembers(ctx, activeKey)
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.GetActiveEvents: %v", err)
		return nil, fmt.Errorf("failed to get active events: %w", err)
	}

	return events, nil
}

func (r *redisQueueRepository) IsActiveEvent(ctx context.Context, eID string) (bool, error) {
	activeKey := r.activeEventsKey()

	exists, err := r.cli.SIsMember(ctx, activeKey, eID)
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.IsActiveEvent: %v", err)
		return false, fmt.Errorf("failed to check active event: %w", err)
	}

	return exists, nil
}

func (r *redisQueueRepository) activeEventsKey() string {
	return "waitroom:active_events"
}
