package metrics

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	pkgLog "github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

func testLogger() pkgLog.Logger {
	return pkgLog.InitializeZapLogger(pkgLog.ZapConfig{Level: "fatal", Mode: "production", Encoding: "json"})
}

// An event that drains its queue leaves the active set while its buyers still
// hold slots; the sampler must keep counting those slots.
func TestSamplerKeepsCountingSlotsAfterQueueDrains(t *testing.T) {
	active := []string{"e1"}
	depth, slots := int64(5), int64(3)

	s := NewQueueSampler(
		func(context.Context) ([]string, error) { return active, nil },
		func(_ context.Context, _ string) (int64, int64, error) { return depth, slots, nil },
		testLogger(),
	)

	s.collect(context.Background())
	if got := testutil.ToFloat64(SlotsInUse.WithLabelValues("e1")); got != 3 {
		t.Fatalf("slots_in_use = %v, want 3", got)
	}

	// Queue empties: waitroom drops e1 from the active set, slots still held.
	active, depth = nil, 0
	s.collect(context.Background())
	if got := testutil.ToFloat64(SlotsInUse.WithLabelValues("e1")); got != 3 {
		t.Fatalf("slots_in_use after drain = %v, want 3", got)
	}

	// Slots released: e1 falls out entirely.
	slots = 0
	s.collect(context.Background())
	if got := testutil.ToFloat64(SlotsInUse.WithLabelValues("e1")); got != 0 {
		t.Fatalf("slots_in_use after release = %v, want 0", got)
	}

	// e1 has left holding, so the next Reset leaves no series behind on either.
	s.collect(context.Background())
	if got := testutil.CollectAndCount(QueueDepth); got != 0 {
		t.Fatalf("queue_depth series = %d, want 0", got)
	}
	if got := testutil.CollectAndCount(SlotsInUse); got != 0 {
		t.Fatalf("slots_in_use series = %d, want 0", got)
	}
}
