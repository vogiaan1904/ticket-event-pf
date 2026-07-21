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
	isProcessingErr    error
	peekErr            error
	removeErr          error

	removeCalls [][]string

	// session IDs announced via the position broadcast (the SSE channel)
	broadcast []string

	// buffered QUEUE_READY payloads awaiting republish
	buffered        []string
	bufferErr       error
	peekBufferedErr error
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

func (f *fakeQueue) IsProcessing(_ context.Context, _, ssID string) (bool, error) {
	if f.isProcessingErr != nil {
		return false, f.isProcessingErr
	}
	return f.processing[ssID], nil
}

func (f *fakeQueue) BufferQueueReady(_ context.Context, payload []byte) error {
	if f.bufferErr != nil {
		return f.bufferErr
	}
	f.buffered = append(f.buffered, string(payload))
	return nil
}

func (f *fakeQueue) PeekBufferedQueueReady(_ context.Context, count int) ([]string, error) {
	if f.peekBufferedErr != nil {
		return nil, f.peekBufferedErr
	}
	return slices.Clone(f.buffered[:min(count, len(f.buffered))]), nil
}

func (f *fakeQueue) TrimBufferedQueueReady(_ context.Context, count int) error {
	f.buffered = slices.Delete(f.buffered, 0, min(count, len(f.buffered)))
	return nil
}

func (f *fakeQueue) EnqueueSession(context.Context, *models.Session) (int64, error) { return 0, nil }
func (f *fakeQueue) DequeueSession(context.Context, string, string) error           { return nil }
func (f *fakeQueue) GetQueueStatus(context.Context, string, *models.Session) (*QueueStatusOutput, error) {
	return nil, nil
}
func (f *fakeQueue) GetQueueInfo(context.Context, string) (*QueueInfoOutput, error) { return nil, nil }
func (f *fakeQueue) RemoveFromProcessing(context.Context, string, string) error     { return nil }
func (f *fakeQueue) GetActiveEvents(context.Context) ([]string, error)              { return nil, nil }
func (f *fakeQueue) PublishPositionUpdate(_ context.Context, u *models.PositionUpdateEvent) error {
	f.broadcast = append(f.broadcast, u.AffectedSessionIDs...)
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

func (f *fakeSessions) UpdateCheckoutToken(_ context.Context, ssID string, token string, expAt time.Time) error {
	if f.updateTokenErr != nil {
		return f.updateTokenErr
	}
	if ss, ok := f.sessions[ssID]; ok {
		now := time.Now()
		ss.CheckoutToken = token
		ss.CheckoutExpiresAt = &expAt
		ss.AdmittedAt = &now
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
			CheckoutTTL:           15 * time.Minute,
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

// --- QUEUE_READY publish failure ---------------------------------------------

type failingPublishProducer struct {
	fakeProducer
	failUntil int // number of publish attempts that fail
	attempts  int
}

func (f *failingPublishProducer) PublishQueueReady(ctx context.Context, e kafka.QueueReadyEvent) error {
	f.attempts++
	if f.attempts <= f.failUntil {
		return errors.New("kafka unreachable")
	}
	return f.fakeProducer.PublishQueueReady(ctx, e)
}

// A failed publish must not un-report an admission that actually happened. The
// naive fix -- returning the error -- sends the caller down the not-admittable
// path, which would drop the user out of the position broadcast: the channel
// they actually learn about their checkout token on.
func TestPublishFailureStillCountsAsAdmitted(t *testing.T) {
	q := newFakeQueue("ss-1")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")}}
	p := &failingPublishProducer{failUntil: 99}

	proc := newTestProcessor(q, s, &fakeProducer{})
	proc.prod = p

	if err := proc.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.sessions["ss-1"].Status != models.SessionStatusAdmitted {
		t.Error("the session was admitted; a failed notification must not undo that")
	}
	if !q.processing["ss-1"] {
		t.Error("the session should hold a checkout slot")
	}
	if slices.Contains(q.queued, "ss-1") {
		t.Error("an admitted session must leave the queue even if its publish failed")
	}
	// The decisive assertion. Returning the publish error instead would send
	// this session down the not-admittable path, excluding it here -- and the
	// position broadcast is how the user actually receives their token.
	if !slices.Contains(q.broadcast, "ss-1") {
		t.Error("an admitted user must still be announced on the position broadcast")
	}

	if len(q.buffered) != 1 {
		t.Errorf("the unpublished event should have been buffered, got %d", len(q.buffered))
	}
}

func TestBufferedEventIsRepublishedOnALaterTick(t *testing.T) {
	q := newFakeQueue("ss-1")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")}}
	p := &failingPublishProducer{failUntil: 1} // first publish fails, then Kafka recovers

	proc := newTestProcessor(q, s, &fakeProducer{})
	proc.prod = p

	if err := proc.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.buffered) != 1 {
		t.Fatalf("expected 1 buffered event, got %d", len(q.buffered))
	}

	// Next tick drains the buffer.
	proc.processAllQueues(context.Background())

	if len(q.buffered) != 0 {
		t.Errorf("buffer should be drained, still holding %d", len(q.buffered))
	}
	if !slices.Contains(p.published, "ss-1") {
		t.Error("the buffered event should have been republished")
	}
}

// A still-broken broker must cost a retry, not the event.
func TestBufferedEventSurvivesAContinuedOutage(t *testing.T) {
	q := newFakeQueue()
	q.buffered = []string{`{"session_id":"ss-1","event_id":"e-1"}`}
	p := &failingPublishProducer{failUntil: 99}

	proc := newTestProcessor(q, &fakeSessions{sessions: map[string]*models.Session{}}, &fakeProducer{})
	proc.prod = p

	proc.processAllQueues(context.Background())

	if len(q.buffered) != 1 {
		t.Errorf("event must stay buffered while the broker is down, got %d", len(q.buffered))
	}
}

// An unparseable entry would otherwise wedge every event behind it.
func TestUnparseableBufferedEventIsDiscarded(t *testing.T) {
	q := newFakeQueue()
	q.buffered = []string{`{not json`, `{"session_id":"ss-2","event_id":"e-1"}`}
	p := &fakeProducer{}

	proc := newTestProcessor(q, &fakeSessions{sessions: map[string]*models.Session{}}, p)

	proc.processAllQueues(context.Background())

	if len(q.buffered) != 0 {
		t.Errorf("buffer should be fully drained, still holding %v", q.buffered)
	}
	if !slices.Contains(p.published, "ss-2") {
		t.Error("the good event behind the poison one must still be published")
	}
}

// If even the buffer write fails the event is genuinely gone -- it must at least
// be counted, not silently dropped.
func TestUnbufferableEventIsCounted(t *testing.T) {
	q := newFakeQueue("ss-1")
	q.bufferErr = errors.New("redis unavailable")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")}}

	proc := newTestProcessor(q, s, &fakeProducer{})
	proc.prod = &failingPublishProducer{failUntil: 99}

	if err := proc.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := proc.GetStatus().ErrorCount; got == 0 {
		t.Error("a lost QUEUE_READY event must increment the error count")
	}
}

// --- half-finished admission recovery ----------------------------------------

// UpdateCheckoutToken commits (session reads as admitted) but the slot write
// fails. The session then holds a token and no slot. Treating that as terminal
// drops the user -- the very loss this processor exists to prevent -- so it must
// be finished on a later tick.
func TestHalfFinishedAdmissionIsResumedNotDropped(t *testing.T) {
	q := newFakeQueue("ss-1")
	q.addToProcessingErr = errors.New("redis unavailable")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")}}
	p := &fakeProducer{}

	proc := newTestProcessor(q, s, p)

	// Tick 1: the slot write fails after the token was persisted.
	if err := proc.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("tick 1: %v", err)
	}

	if !slices.Contains(q.queued, "ss-1") {
		t.Fatal("the user must stay queued when the slot write fails")
	}
	if s.sessions["ss-1"].Status != models.SessionStatusAdmitted {
		t.Fatal("precondition: the session is left admitted without a slot")
	}
	if q.processing["ss-1"] {
		t.Fatal("precondition: no slot was taken")
	}

	// Tick 2: Redis recovers. The half-finished admission must be completed,
	// not classified as stale.
	q.addToProcessingErr = nil
	if err := proc.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("tick 2: %v", err)
	}

	if !q.processing["ss-1"] {
		t.Error("the resumed admission should have claimed a checkout slot")
	}
	if slices.Contains(q.queued, "ss-1") {
		t.Error("the resumed admission should have left the queue")
	}
	if !slices.Contains(p.published, "ss-1") {
		t.Error("the resumed admission should have published QUEUE_READY")
	}
	if !slices.Contains(q.broadcast, "ss-1") {
		t.Error("the resumed admission should be announced on the position broadcast")
	}
}

// The resumed admission must reuse the token already handed out, not mint a new
// one -- the old token may already be in the user's hands.
func TestResumedAdmissionReusesTheExistingToken(t *testing.T) {
	expAt := time.Now().Add(10 * time.Minute)
	ss := queuedSession("ss-1")
	ss.Status = models.SessionStatusAdmitted
	ss.CheckoutToken = "already-issued-token"
	ss.CheckoutExpiresAt = &expAt

	q := newFakeQueue("ss-1")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": ss}}
	s.generateTokenErr = errors.New("must not mint a new token")

	proc := newTestProcessor(q, s, &fakeProducer{})
	if err := proc.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !q.processing["ss-1"] {
		t.Error("the half-finished admission should have been completed")
	}
}

// A transient failure while checking slot state must not be read as "no slot".
func TestIsProcessingFailureLeavesUserQueued(t *testing.T) {
	expAt := time.Now().Add(10 * time.Minute)
	ss := queuedSession("ss-1")
	ss.Status = models.SessionStatusAdmitted
	ss.CheckoutToken = "tok"
	ss.CheckoutExpiresAt = &expAt

	q := newFakeQueue("ss-1")
	q.isProcessingErr = errors.New("redis unavailable")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": ss}}

	if err := newTestProcessor(q, s, &fakeProducer{}).ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(q.queued, "ss-1") {
		t.Error("an unreadable slot state is transient; the user must stay queued")
	}
}

// An admitted session whose checkout window has already lapsed is genuinely
// finished -- resuming it would hand back an expired token.
func TestExpiredCheckoutIsNotResumed(t *testing.T) {
	expAt := time.Now().Add(-time.Minute)
	ss := queuedSession("ss-1")
	ss.Status = models.SessionStatusAdmitted
	ss.CheckoutToken = "stale-tok"
	ss.CheckoutExpiresAt = &expAt

	q := newFakeQueue("ss-1")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": ss}}

	if err := newTestProcessor(q, s, &fakeProducer{}).ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if q.processing["ss-1"] {
		t.Error("an expired checkout must not be resumed")
	}
	if slices.Contains(q.queued, "ss-1") {
		t.Error("an expired checkout is terminal and should be purged from the queue")
	}
}
