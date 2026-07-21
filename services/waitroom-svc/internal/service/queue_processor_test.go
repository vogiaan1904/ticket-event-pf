package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

// --- fakes -------------------------------------------------------------------

// fakeQueue models the queue as an ordered slice and the processing set as a
// map, so a test can assert exactly who is still queued after a tick.
type fakeQueue struct {
	queued     []string
	processing map[string]bool

	addToProcessingErr error
	peekErr            error
	removeErr          error

	removeCalls [][]string
}

func newFakeQueue(queued ...string) *fakeQueue {
	return &fakeQueue{queued: slices.Clone(queued), processing: map[string]bool{}}
}

func (f *fakeQueue) PeekQueue(_ context.Context, _ string, count int) ([]string, error) {
	if f.peekErr != nil {
		return nil, f.peekErr
	}
	return slices.Clone(f.queued[:min(count, len(f.queued))]), nil
}

func (f *fakeQueue) RemoveFromQueue(_ context.Context, _ string, ssIDs ...string) error {
	f.removeCalls = append(f.removeCalls, slices.Clone(ssIDs))
	if f.removeErr != nil {
		return f.removeErr
	}
	f.queued = slices.DeleteFunc(f.queued, func(id string) bool {
		return slices.Contains(ssIDs, id)
	})
	return nil
}

func (f *fakeQueue) AddToProcessing(_ context.Context, _, ssID string, _ time.Duration) error {
	if f.addToProcessingErr != nil {
		return f.addToProcessingErr
	}
	f.processing[ssID] = true
	return nil
}

func (f *fakeQueue) GetProcessingCount(context.Context, string) (int64, error) {
	return int64(len(f.processing)), nil
}

func (f *fakeQueue) EnqueueSession(context.Context, *models.Session) (int64, error) { return 0, nil }
func (f *fakeQueue) DequeueSession(context.Context, string, string) error           { return nil }
func (f *fakeQueue) GetQueueStatus(context.Context, string, *models.Session) (*QueueStatusOutput, error) {
	return nil, nil
}
func (f *fakeQueue) GetQueueInfo(context.Context, string) (*QueueInfoOutput, error) { return nil, nil }
func (f *fakeQueue) RemoveFromProcessing(context.Context, string, string) error     { return nil }
func (f *fakeQueue) GetActiveEvents(context.Context) ([]string, error)              { return nil, nil }
func (f *fakeQueue) PublishPositionUpdate(context.Context, *models.PositionUpdateEvent) error {
	return nil
}
func (f *fakeQueue) SubscribeToPositionUpdates(context.Context, string) (PositionUpdateSubscription, error) {
	return nil, nil
}

type fakeSessions struct {
	sessions map[string]*models.Session

	getErr            error
	updateTokenErr    error
	generateTokenErr  error
	statusUpdatesSeen []string
}

func (f *fakeSessions) GetSession(_ context.Context, ssID string) (*models.Session, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	ss, ok := f.sessions[ssID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return ss, nil
}

func (f *fakeSessions) GenerateCheckoutToken(context.Context, *models.Session) (string, error) {
	if f.generateTokenErr != nil {
		return "", f.generateTokenErr
	}
	return "token", nil
}

func (f *fakeSessions) UpdateCheckoutToken(_ context.Context, ssID string, _ string, _ time.Time) error {
	if f.updateTokenErr != nil {
		return f.updateTokenErr
	}
	if ss, ok := f.sessions[ssID]; ok {
		ss.Status = models.SessionStatusAdmitted
	}
	return nil
}

func (f *fakeSessions) UpdateSessionStatus(_ context.Context, ssID string, st models.SessionStatus) error {
	f.statusUpdatesSeen = append(f.statusUpdatesSeen, ssID)
	if ss, ok := f.sessions[ssID]; ok {
		ss.Status = st
	}
	return nil
}

func (f *fakeSessions) CreateSession(context.Context, string, string, string, string) (*models.Session, error) {
	return nil, nil
}
func (f *fakeSessions) UpdateSession(context.Context, *models.Session) error          { return nil }
func (f *fakeSessions) ValidateSession(context.Context, string) error                 { return nil }
func (f *fakeSessions) InvalidateCheckoutToken(context.Context, string, string) error { return nil }
func (f *fakeSessions) ValidateCheckoutToken(context.Context, string) error           { return nil }

type fakeProducer struct{ published []string }

func (f *fakeProducer) PublishQueueReady(_ context.Context, e kafka.QueueReadyEvent) error {
	f.published = append(f.published, e.SessionID)
	return nil
}
func (f *fakeProducer) PublishQueueJoined(context.Context, kafka.QueueJoinedEvent) error { return nil }
func (f *fakeProducer) PublishQueueLeft(context.Context, kafka.QueueLeftEvent) error     { return nil }
func (f *fakeProducer) Close() error                                                     { return nil }

// --- helpers -----------------------------------------------------------------

func queuedSession(id string) *models.Session {
	return &models.Session{
		ID:        id,
		UserID:    "u-" + id,
		EventID:   "e-1",
		Status:    models.SessionStatusQueued,
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func newTestProcessor(q *fakeQueue, s *fakeSessions, p *fakeProducer) *queueProcessor {
	return &queueProcessor{
		qSvc:  q,
		ssSvc: s,
		prod:  p,
		l:     logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"}),
		cfg: ProcessorConfig{
			MaxConcurrentPerEvent: 10,
			BatchSize:             10,
			RetryAttempts:         2,
			RetryDelay:            time.Millisecond,
		},
		stopCh: make(chan struct{}),
	}
}

// --- tests -------------------------------------------------------------------

func TestAdmittedSessionsLeaveTheQueue(t *testing.T) {
	q := newFakeQueue("ss-1", "ss-2")
	s := &fakeSessions{sessions: map[string]*models.Session{
		"ss-1": queuedSession("ss-1"),
		"ss-2": queuedSession("ss-2"),
	}}
	p := &fakeProducer{}

	if err := newTestProcessor(q, s, p).ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(q.queued) != 0 {
		t.Errorf("admitted sessions should have left the queue, still queued: %v", q.queued)
	}
	if len(p.published) != 2 {
		t.Errorf("expected 2 QUEUE_READY events, got %d", len(p.published))
	}
}

// The bug: a transient failure used to drop the user from the queue for good.
// They must stay queued, at their original position, for the next tick.
func TestTransientFailureLeavesUserQueuedAtSamePosition(t *testing.T) {
	q := newFakeQueue("ss-1", "ss-2", "ss-3")
	q.addToProcessingErr = errors.New("redis unavailable")

	s := &fakeSessions{sessions: map[string]*models.Session{
		"ss-1": queuedSession("ss-1"),
		"ss-2": queuedSession("ss-2"),
		"ss-3": queuedSession("ss-3"),
	}}

	if err := newTestProcessor(q, s, &fakeProducer{}).ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"ss-1", "ss-2", "ss-3"}
	if !slices.Equal(q.queued, want) {
		t.Errorf("queue = %v, want %v -- transient failures must not drop users", q.queued, want)
	}

	for _, call := range q.removeCalls {
		if len(call) > 0 {
			t.Errorf("nothing should have been removed, but RemoveFromQueue got %v", call)
		}
	}
}

func TestTransientGetSessionFailureLeavesUserQueued(t *testing.T) {
	q := newFakeQueue("ss-1")
	s := &fakeSessions{
		sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")},
		getErr:   errors.New("redis unavailable"),
	}

	if err := newTestProcessor(q, s, &fakeProducer{}).ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(q.queued, []string{"ss-1"}) {
		t.Errorf("queue = %v, want [ss-1]", q.queued)
	}
}

// A session that is genuinely no longer a queue member must be purged, or it
// would block the head of the queue forever.
func TestStaleEntriesArePurged(t *testing.T) {
	tests := []struct {
		name    string
		session *models.Session // nil => not in Redis at all
	}{
		{name: "session vanished (TTL lapsed)", session: nil},
		{name: "already admitted", session: &models.Session{
			ID: "ss-1", Status: models.SessionStatusAdmitted, ExpiresAt: time.Now().Add(time.Hour)}},
		{name: "abandoned", session: &models.Session{
			ID: "ss-1", Status: models.SessionStatusAbandoned, ExpiresAt: time.Now().Add(time.Hour)}},
		{name: "session past its expiry", session: &models.Session{
			ID: "ss-1", Status: models.SessionStatusQueued, ExpiresAt: time.Now().Add(-time.Minute)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newFakeQueue("ss-1", "ss-2")
			s := &fakeSessions{sessions: map[string]*models.Session{"ss-2": queuedSession("ss-2")}}
			if tt.session != nil {
				s.sessions["ss-1"] = tt.session
			}

			if err := newTestProcessor(q, s, &fakeProducer{}).ProcessEventQueue(context.Background(), "e-1"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if slices.Contains(q.queued, "ss-1") {
				t.Errorf("stale entry ss-1 should have been purged, queue = %v", q.queued)
			}
		})
	}
}

// A head-of-queue transient failure must not stop the users behind it from
// being admitted into the free slots.
func TestTransientFailureDoesNotBlockTheRestOfTheBatch(t *testing.T) {
	q := newFakeQueue("ss-1", "ss-2")
	s := &fakeSessions{sessions: map[string]*models.Session{
		"ss-1": queuedSession("ss-1"),
		"ss-2": queuedSession("ss-2"),
	}}
	p := &fakeProducer{}

	proc := newTestProcessor(q, s, p)
	// Only ss-1 fails, and only transiently.
	proc.qSvc = &selectiveFailQueue{fakeQueue: q, failFor: "ss-1"}

	if err := proc.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(q.queued, "ss-1") {
		t.Error("ss-1 failed transiently and must stay queued")
	}
	if slices.Contains(q.queued, "ss-2") {
		t.Error("ss-2 should have been admitted despite ss-1 failing")
	}
	if !slices.Contains(p.published, "ss-2") {
		t.Error("ss-2 should have received QUEUE_READY")
	}
}

type selectiveFailQueue struct {
	*fakeQueue
	failFor string
}

func (s *selectiveFailQueue) AddToProcessing(ctx context.Context, eID, ssID string, ttl time.Duration) error {
	if ssID == s.failFor {
		return errors.New("redis unavailable")
	}
	return s.fakeQueue.AddToProcessing(ctx, eID, ssID, ttl)
}

// If the queue removal itself fails, the admitted sessions stay queued. That is
// safe -- they hold their slots and the next tick sees them as not-admittable
// and drops them then. What must never happen is losing them.
func TestRemovalFailureIsSelfCorrecting(t *testing.T) {
	q := newFakeQueue("ss-1")
	q.removeErr = errors.New("redis unavailable")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")}}

	proc := newTestProcessor(q, s, &fakeProducer{})
	if err := proc.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(q.queued, "ss-1") {
		t.Fatal("precondition: removal failed so ss-1 is still queued")
	}

	// Next tick: the session is now admitted, so it is not admittable and gets
	// dropped rather than admitted twice.
	q.removeErr = nil
	if err := proc.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error on second tick: %v", err)
	}

	if slices.Contains(q.queued, "ss-1") {
		t.Errorf("second tick should have dropped the settled entry, queue = %v", q.queued)
	}
}

func TestNoSlotsAvailableLeavesQueueUntouched(t *testing.T) {
	q := newFakeQueue("ss-1")
	for i := range 10 {
		q.processing[string(rune('a'+i))] = true
	}
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")}}

	if err := newTestProcessor(q, s, &fakeProducer{}).ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(q.queued, []string{"ss-1"}) {
		t.Errorf("queue = %v, want [ss-1] untouched when no slots are free", q.queued)
	}
}
