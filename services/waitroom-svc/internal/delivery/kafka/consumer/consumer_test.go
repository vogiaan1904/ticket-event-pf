package consumer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type fakeDLQ struct {
	calls    atomic.Int32
	attempts int
	reason   string
	err      error
}

func (f *fakeDLQ) PublishDeadLetter(_ context.Context, _ *sarama.ConsumerMessage, reason string, attempts int) error {
	f.calls.Add(1)
	f.reason = reason
	f.attempts = attempts
	return f.err
}

// newTestConsumer builds a Consumer whose message handling is replaced by the
// given function, so delivery semantics can be tested without Kafka or Redis.
func newTestConsumer(t *testing.T, dlq *fakeDLQ, policy RetryPolicy, process func(int) error) *Consumer {
	t.Helper()

	l := logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})

	var calls atomic.Int32
	c := &Consumer{dlq: dlq, policy: policy, l: l}
	c.process = func(context.Context, *sarama.ConsumerMessage) error {
		return process(int(calls.Add(1)))
	}

	return c
}

func testMessage() *sarama.ConsumerMessage {
	return &sarama.ConsumerMessage{
		Topic:     "checkout.completed",
		Partition: 3,
		Offset:    42,
		Value:     []byte(`{"session_id":"ss-1"}`),
	}
}

func fastPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 4, BaseDelay: time.Millisecond}
}

func TestSuccessCommitsImmediately(t *testing.T) {
	dlq := &fakeDLQ{}
	c := newTestConsumer(t, dlq, fastPolicy(), func(int) error { return nil })

	commit, err := c.deliver(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !commit {
		t.Error("a processed message must commit its offset")
	}
	if dlq.calls.Load() != 0 {
		t.Error("success must not touch the DLQ")
	}
}

// The bug this fixes: a failed message used to be skipped, and the next
// success committed straight past it.
func TestTransientFailureIsRetriedThenSucceeds(t *testing.T) {
	dlq := &fakeDLQ{}
	c := newTestConsumer(t, dlq, fastPolicy(), func(attempt int) error {
		if attempt < 3 {
			return errors.New("redis unavailable")
		}
		return nil
	})

	commit, err := c.deliver(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !commit {
		t.Error("a message that eventually succeeded must commit")
	}
	if dlq.calls.Load() != 0 {
		t.Error("a message that succeeded on retry must not be dead-lettered")
	}
}

func TestExhaustedTransientFailureGoesToDLQAndCommits(t *testing.T) {
	dlq := &fakeDLQ{}
	policy := fastPolicy()
	c := newTestConsumer(t, dlq, policy, func(int) error { return errors.New("redis unavailable") })

	commit, err := c.deliver(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !commit {
		t.Error("a parked message must commit so the partition keeps moving")
	}
	if dlq.calls.Load() != 1 {
		t.Fatalf("expected exactly 1 DLQ publish, got %d", dlq.calls.Load())
	}
	if dlq.attempts != policy.MaxAttempts {
		t.Errorf("DLQ recorded %d attempts, want %d", dlq.attempts, policy.MaxAttempts)
	}
}

// An undecodable payload fails identically forever; retrying it just delays
// every checkout release behind it.
func TestPermanentFailureSkipsRetriesAndGoesToDLQ(t *testing.T) {
	dlq := &fakeDLQ{}
	var calls atomic.Int32
	c := newTestConsumer(t, dlq, fastPolicy(), func(int) error {
		calls.Add(1)
		return Permanent(errors.New("invalid character 'x'"))
	})

	commit, err := c.deliver(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !commit {
		t.Error("a parked poison message must commit")
	}
	if calls.Load() != 1 {
		t.Errorf("permanent failure was attempted %d times, want 1", calls.Load())
	}
	if dlq.calls.Load() != 1 {
		t.Errorf("expected the poison message to be dead-lettered")
	}
}

// If the message can neither be processed nor parked, committing would lose it.
// Refusing to commit stalls the partition instead -- loud, and recoverable.
func TestDLQFailureRefusesToCommit(t *testing.T) {
	dlq := &fakeDLQ{err: errors.New("kafka unreachable")}
	c := newTestConsumer(t, dlq, fastPolicy(), func(int) error { return errors.New("redis unavailable") })

	commit, err := c.deliver(context.Background(), testMessage())
	if err == nil {
		t.Fatal("expected the DLQ failure to surface")
	}
	if commit {
		t.Error("must not commit a message that was neither processed nor parked")
	}
}

// On shutdown or rebalance the offset must stay uncommitted so whoever picks up
// the partition next sees the message again.
func TestCancellationDuringRetryDoesNotCommit(t *testing.T) {
	dlq := &fakeDLQ{}
	ctx, cancel := context.WithCancel(context.Background())

	c := newTestConsumer(t, dlq, RetryPolicy{MaxAttempts: 5, BaseDelay: time.Hour},
		func(attempt int) error {
			if attempt == 1 {
				cancel()
			}
			return errors.New("redis unavailable")
		})

	commit, err := c.deliver(ctx, testMessage())
	if err != nil {
		t.Fatalf("cancellation is not an error: %v", err)
	}
	if commit {
		t.Error("must not commit when interrupted mid-retry")
	}
	if dlq.calls.Load() != 0 {
		t.Error("an interrupted message must be redelivered, not dead-lettered")
	}
}

func TestRetryDelayGrowsExponentially(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 5, BaseDelay: 100 * time.Millisecond}

	for attempt, want := range map[int]time.Duration{
		1: 100 * time.Millisecond,
		2: 200 * time.Millisecond,
		3: 400 * time.Millisecond,
		4: 800 * time.Millisecond,
	} {
		if got := p.delayFor(attempt); got != want {
			t.Errorf("delayFor(%d) = %v, want %v", attempt, got, want)
		}
	}
}

func TestZeroAttemptPolicyStillProcessesOnce(t *testing.T) {
	dlq := &fakeDLQ{}
	var calls atomic.Int32
	c := newTestConsumer(t, dlq, RetryPolicy{}, func(int) error {
		calls.Add(1)
		return nil
	})

	if _, err := c.deliver(context.Background(), testMessage()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("processed %d times, want 1", calls.Load())
	}
}
