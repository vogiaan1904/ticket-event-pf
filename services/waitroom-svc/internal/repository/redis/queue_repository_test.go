package repository

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/redis"
)

// Set WAITROOM_TEST_REDIS_ADDR to run these. e.g.
//
//	docker run -d --name tb-waitroom-test-redis -p 63799:6379 redis:7-alpine
//	WAITROOM_TEST_REDIS_ADDR=localhost:63799 go test ./internal/repository/redis/
func newTestRepo(t *testing.T) (QueueRepository, *redis.Client) {
	t.Helper()

	addr := os.Getenv("WAITROOM_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("WAITROOM_TEST_REDIS_ADDR not set; skipping Redis integration test")
	}

	cli := redis.NewClient(config.RedisConfig{Addr: addr, PoolSize: 5, MinIdleConns: 1})
	if err := cli.Ping(context.Background()); err != nil {
		t.Fatalf("cannot reach test redis at %s: %v", addr, err)
	}

	l := logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})

	return NewRedisQueueRepository(cli, l), cli
}

// The central invariant: each checkout slot expires on its own schedule.
//
// The previous implementation was a SET with Expire() on the whole key, so
// every new admission pushed back the expiry of every slot already in flight.
// Under sustained traffic no slot ever expired and the queue starved.
func TestProcessingSlotsExpireIndependently(t *testing.T) {
	repo, cli := newTestRepo(t)
	ctx := context.Background()
	eID := "evt-independent-expiry"
	defer cli.Del(ctx, "waitroom:"+eID+":checkouts")

	if err := repo.AddToProcessing(ctx, eID, "ss-short", 400*time.Millisecond); err != nil {
		t.Fatalf("add short slot: %v", err)
	}

	// A later admission must not extend the earlier slot's life.
	time.Sleep(200 * time.Millisecond)
	if err := repo.AddToProcessing(ctx, eID, "ss-long", 10*time.Second); err != nil {
		t.Fatalf("add long slot: %v", err)
	}

	count, err := repo.GetProcessingCount(ctx, eID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 slots in flight, got %d", count)
	}

	// Past the short slot's expiry, but well inside the long slot's.
	time.Sleep(400 * time.Millisecond)

	count, err = repo.GetProcessingCount(ctx, eID)
	if err != nil {
		t.Fatalf("count after expiry: %v", err)
	}
	if count != 1 {
		t.Errorf("expected the short slot to expire alone, got count=%d", count)
	}

	// ...and the survivor must be the long one, not a wholesale key wipe.
	stillHeld, err := repo.IsProcessing(ctx, eID, "ss-long")
	if err != nil {
		t.Fatalf("is processing: %v", err)
	}
	if !stillHeld {
		t.Error("the long-lived slot was dropped; expiry is not per-slot")
	}

	expired, err := repo.IsProcessing(ctx, eID, "ss-short")
	if err != nil {
		t.Fatalf("is processing: %v", err)
	}
	if expired {
		t.Error("the short slot should have expired")
	}
}

// An abandoned checkout must free its slot with no downstream event at all --
// this is what stops a stalled queue from needing manual intervention.
func TestAbandonedSlotIsReclaimedWithoutAnEvent(t *testing.T) {
	repo, cli := newTestRepo(t)
	ctx := context.Background()
	eID := "evt-abandoned"
	defer cli.Del(ctx, "waitroom:"+eID+":checkouts")

	for _, id := range []string{"ss-1", "ss-2", "ss-3"} {
		if err := repo.AddToProcessing(ctx, eID, id, 300*time.Millisecond); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}

	time.Sleep(500 * time.Millisecond)

	count, err := repo.GetProcessingCount(ctx, eID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected all abandoned slots reclaimed, got %d still held", count)
	}
}

func TestRemoveFromProcessingFreesSlotImmediately(t *testing.T) {
	repo, cli := newTestRepo(t)
	ctx := context.Background()
	eID := "evt-remove"
	defer cli.Del(ctx, "waitroom:"+eID+":checkouts")

	if err := repo.AddToProcessing(ctx, eID, "ss-1", time.Minute); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.AddToProcessing(ctx, eID, "ss-2", time.Minute); err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := repo.RemoveFromProcessing(ctx, eID, "ss-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	count, err := repo.GetProcessingCount(ctx, eID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 slot left, got %d", count)
	}

	held, err := repo.IsProcessing(ctx, eID, "ss-1")
	if err != nil {
		t.Fatalf("is processing: %v", err)
	}
	if held {
		t.Error("removed session still reported as holding a slot")
	}
}

// Re-admitting the same session must refresh its slot, not double-count it.
func TestReAddingSessionDoesNotDoubleCount(t *testing.T) {
	repo, cli := newTestRepo(t)
	ctx := context.Background()
	eID := "evt-readd"
	defer cli.Del(ctx, "waitroom:"+eID+":checkouts")

	for range 3 {
		if err := repo.AddToProcessing(ctx, eID, "ss-1", time.Minute); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	count, err := repo.GetProcessingCount(ctx, eID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 slot, got %d", count)
	}
}

// Reading the head of the queue must not remove anything -- that is what lets
// the processor retry a transient admission failure at the same position.
func TestGetQueueMembersIsNonDestructiveAndOrdered(t *testing.T) {
	repo, cli := newTestRepo(t)
	ctx := context.Background()
	eID := "evt-peek"
	defer cli.Del(ctx, "waitroom:"+eID+":queue")

	base := time.Now()
	for i, id := range []string{"ss-1", "ss-2", "ss-3"} {
		ss := &models.Session{ID: id, EventID: eID, QueuedAt: base.Add(time.Duration(i) * time.Second)}
		if err := repo.AddToQueue(ctx, eID, ss); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}

	for range 2 {
		got, err := repo.GetQueueMembers(ctx, eID, 0, 1)
		if err != nil {
			t.Fatalf("peek: %v", err)
		}
		if !slices.Equal(got, []string{"ss-1", "ss-2"}) {
			t.Fatalf("peek = %v, want [ss-1 ss-2]", got)
		}
	}

	length, err := repo.GetQueueLength(ctx, eID)
	if err != nil {
		t.Fatalf("length: %v", err)
	}
	if length != 3 {
		t.Errorf("peeking removed entries: length = %d, want 3", length)
	}
}

func TestRemoveFromQueueDropsExactlyTheGivenMembers(t *testing.T) {
	repo, cli := newTestRepo(t)
	ctx := context.Background()
	eID := "evt-remove-batch"
	defer cli.Del(ctx, "waitroom:"+eID+":queue")

	base := time.Now()
	for i, id := range []string{"ss-1", "ss-2", "ss-3"} {
		ss := &models.Session{ID: id, EventID: eID, QueuedAt: base.Add(time.Duration(i) * time.Second)}
		if err := repo.AddToQueue(ctx, eID, ss); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}

	if err := repo.RemoveFromQueue(ctx, eID, "ss-1", "ss-3"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got, err := repo.GetQueueMembers(ctx, eID, 0, -1)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if !slices.Equal(got, []string{"ss-2"}) {
		t.Errorf("queue = %v, want [ss-2]", got)
	}

	// Removing nothing must be a no-op, not a full-key operation.
	if err := repo.RemoveFromQueue(ctx, eID); err != nil {
		t.Fatalf("empty remove: %v", err)
	}
	if length, _ := repo.GetQueueLength(ctx, eID); length != 1 {
		t.Errorf("empty remove changed the queue: length = %d, want 1", length)
	}
}

func TestGetProcessingCountOnUnknownEvent(t *testing.T) {
	repo, _ := newTestRepo(t)

	count, err := repo.GetProcessingCount(context.Background(), "evt-does-not-exist")
	if err != nil {
		t.Fatalf("count on missing key should not error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}
