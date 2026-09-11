package metrics

import (
	"context"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

// ActiveEventsFn lists the events with a non-empty queue.
type ActiveEventsFn func(ctx context.Context) ([]string, error)

// SampleEventFn reads one event's queue depth and held checkout slots.
type SampleEventFn func(ctx context.Context, eventID string) (depth, slots int64, err error)

// QueueSampler refreshes the queue gauges on a timer.
// Why: the queue lives in Redis and shrinks without a gRPC call, so a gauge
// written only on request would report a depth minutes old -- and a stale gauge
// reads exactly like a healthy one.
type QueueSampler struct {
	active ActiveEventsFn
	sample SampleEventFn
	l      logger.Logger
	// Events that left the active set still holding slots. Redis cannot be
	// enumerated, so an event drops out the tick after its slots reach zero.
	holding map[string]struct{}
}

func NewQueueSampler(active ActiveEventsFn, sample SampleEventFn, l logger.Logger) *QueueSampler {
	return &QueueSampler{active: active, sample: sample, l: l, holding: map[string]struct{}{}}
}

// Run samples every interval until ctx is cancelled.
func (s *QueueSampler) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.collect(ctx)
		}
	}
}

func (s *QueueSampler) collect(ctx context.Context) {
	ids, err := s.active(ctx)
	if err != nil {
		s.l.Warnf(ctx, "metrics.QueueSampler: failed to list active events: %v", err)
		return
	}

	// An event whose queue drained leaves the active set while its buyers still
	// hold slots, so carry it until its own count says zero.
	for id := range s.holding {
		ids = appendMissing(ids, id)
	}

	// Reset drops the series of events that are gone, which is what bounds this
	// gauge's cardinality to the events currently in play.
	QueueDepth.Reset()

	var total int64
	for _, id := range ids {
		depth, slots, err := s.sample(ctx, id)
		if err != nil {
			s.l.Warnf(ctx, "metrics.QueueSampler: failed to sample event_id=%s: %v", id, err)
			continue
		}
		QueueDepth.WithLabelValues(id).Set(float64(depth))
		total += slots

		if slots > 0 {
			s.holding[id] = struct{}{}
		} else {
			delete(s.holding, id)
		}
	}
	SlotsInUse.Set(float64(total))
}

func appendMissing(ids []string, id string) []string {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}
