package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type QueueProcessor interface {
	Start(ctx context.Context) error
	Stop() error
	ProcessEventQueue(ctx context.Context, eventID string) error
	GetStatus() ProcessorStatus
}

type ProcessorStatus struct {
	IsRunning     bool      `json:"is_running"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	LastProcessed time.Time `json:"last_processed,omitempty"`
	EventsActive  int       `json:"events_active"`
	TotalAdmitted int64     `json:"total_admitted"`
	ErrorCount    int64     `json:"error_count"`
}

type queueProcessor struct {
	qSvc          QueueService
	ssSvc         SessionService
	stock         *StockGate
	prod          producer.Producer
	l             logger.Logger
	cfg           ProcessorConfig
	mu            sync.RWMutex
	isRunning     bool
	startedAt     time.Time
	stopCh        chan struct{}
	ticker        *time.Ticker
	wg            sync.WaitGroup
	lastProcessed time.Time
	totalAdmitted int64
	errorCount    int64
}

type ProcessorConfig struct {
	ProcessInterval       time.Duration // How often to process queues
	BatchSize             int           // Max users to admit per batch
	RetryAttempts         int           // Retry attempts for failed operations
	RetryDelay            time.Duration // Delay between retries
	CheckoutTTL           time.Duration // How long an admitted user holds a slot
	ShutdownTimeout       time.Duration // Max time to wait for graceful shutdown
	EnableMetrics         bool          // Enable detailed metrics collection
	MaxProcessingDuration time.Duration // Max time for processing all events
}

func NewQueueProcessor(
	qSvc QueueService,
	ssSvc SessionService,
	stock *StockGate,
	prod producer.Producer,
	l logger.Logger,
	cfg config.QueueConfig,
	jwtCfg config.JWTConfig,
) QueueProcessor {
	return &queueProcessor{
		qSvc:  qSvc,
		ssSvc: ssSvc,
		stock: stock,
		prod:  prod,
		l:     l,
		cfg: ProcessorConfig{
			ProcessInterval: cfg.ProcessInterval,
			BatchSize:       cfg.DefaultReleaseRate,
			RetryAttempts:   3,
			RetryDelay:      time.Second,
			// The slot TTL and the token lifetime must be the same window --
			// a slot outliving its token holds capacity nobody can use.
			CheckoutTTL:           jwtCfg.Expiry,
			ShutdownTimeout:       30 * time.Second,
			EnableMetrics:         true,
			MaxProcessingDuration: 30 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

func (qp *queueProcessor) Start(ctx context.Context) error {
	qp.mu.Lock()
	defer qp.mu.Unlock()

	if qp.isRunning {
		return errors.New("queue processor is already running")
	}

	qp.l.Infof(ctx, "Starting queue processor - interval: %v, batch_size: %d",
		qp.cfg.ProcessInterval, qp.cfg.BatchSize)

	qp.isRunning = true
	qp.startedAt = time.Now()
	qp.ticker = time.NewTicker(qp.cfg.ProcessInterval)

	qp.wg.Add(1)
	go qp.processLoop(ctx)

	qp.l.Infof(ctx, "Queue processor started successfully")
	return nil
}

func (qp *queueProcessor) Stop() error {
	qp.mu.Lock()
	defer qp.mu.Unlock()

	if !qp.isRunning {
		return errors.New("queue processor is not running")
	}

	qp.l.Infof(context.Background(), "Stopping queue processor...")

	close(qp.stopCh)

	if qp.ticker != nil {
		qp.ticker.Stop()
	}

	done := make(chan struct{})
	go func() {
		qp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		qp.l.Infof(context.Background(), "Queue processor stopped gracefully")
	case <-time.After(qp.cfg.ShutdownTimeout):
		qp.l.Warnf(context.Background(), "Queue processor shutdown timeout exceeded")
	}

	qp.isRunning = false
	return nil
}

func (qp *queueProcessor) processLoop(ctx context.Context) {
	defer qp.wg.Done()

	qp.l.Infof(ctx, "Queue processor loop started")

	for {
		select {
		case <-ctx.Done():
			qp.l.Infof(ctx, "Queue processor stopped due to context cancellation")
			return
		case <-qp.stopCh:
			qp.l.Infof(ctx, "Queue processor stopped due to stop signal")
			return
		case <-qp.ticker.C:
			qp.processAllQueues(ctx)
		}
	}
}

func (qp *queueProcessor) processAllQueues(ctx context.Context) {
	startTime := time.Now()
	defer func() {
		qp.mu.Lock()
		qp.lastProcessed = time.Now()
		qp.mu.Unlock()

		duration := time.Since(startTime)
		if duration > qp.cfg.MaxProcessingDuration {
			qp.l.Warnf(ctx, "Queue processing took longer than expected - duration: %v, max: %v",
				duration, qp.cfg.MaxProcessingDuration)
		}
	}()

	qp.drainBufferedQueueReady(ctx)

	activeEvents, err := qp.getActiveEvents(ctx)
	if err != nil {
		qp.incrementErrorCount()
		qp.l.Errorf(ctx, "Failed to get active events: %v", err)
		return
	}

	if len(activeEvents) == 0 {
		return
	}

	qp.l.Debugf(ctx, "Processing queues for active events, event_count: %d", len(activeEvents))

	for _, eventID := range activeEvents {
		if err := qp.ProcessEventQueue(ctx, eventID); err != nil {
			qp.incrementErrorCount()
			qp.l.Errorf(ctx, "Failed to process queue for event: %v", err)
		}
	}
}

func (qp *queueProcessor) ProcessEventQueue(ctx context.Context, eventID string) error {
	processingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Claim/ack, not pop: an entry leaves the queue only on a terminal outcome,
	// so a transient failure retries the same user at the same position.
	// Invariant: a session is never in neither the queue nor the processing set.
	ssIDs, err := qp.qSvc.PeekQueue(processingCtx, eventID, qp.cfg.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to peek queue: %w", err)
	}

	if len(ssIDs) == 0 {
		qp.l.Debugf(processingCtx, "No sessions to process in queue, event_id: %s", eventID)
		return nil
	}

	// Asked only with someone due, so an empty line costs inventory nothing.
	stock := qp.stock.Stock(processingCtx, eventID)
	switch stock.Door {
	case DoorPaused:
		qp.l.Debugf(processingCtx, "Door paused, no ticket available - event_id: %s", eventID)
		return qp.dropDeadHead(processingCtx, eventID)
	case DoorSoldOut:
		return qp.closeLine(processingCtx, eventID)
	}

	inside, err := qp.qSvc.GetProcessingCount(processingCtx, eventID)
	if err != nil {
		return fmt.Errorf("failed to get processing count: %w", err)
	}

	n := admitCount(len(ssIDs), stock, inside)
	if n == 0 {
		qp.l.Debugf(processingCtx, "No ticket left beyond the buyers inside - event_id: %s, inside: %d, available: %d", eventID, inside, stock.Available)
		return nil
	}
	ssIDs = ssIDs[:n]

	qp.l.Infof(processingCtx, "Starting batch admission, event_id: %s, session_count: %d", eventID, len(ssIDs))

	admittedSsIDs := make([]string, 0, len(ssIDs))
	staleSsIDs := make([]string, 0)

	for _, sessionID := range ssIDs {
		err := qp.admitUserToCheckout(processingCtx, eventID, sessionID)

		switch {
		case err == nil:
			admittedSsIDs = append(admittedSsIDs, sessionID)

		case errors.Is(err, ErrSessionNotAdmittable):
			qp.l.Infof(processingCtx, "Dropping stale queue entry - event_id: %s, session_id: %s, reason: %v",
				eventID, sessionID, err)
			staleSsIDs = append(staleSsIDs, sessionID)

		default:
			// Transient: leave it queued; the next tick retries the same position.
			qp.l.Errorf(processingCtx, "Failed to admit user, leaving queued for retry - event_id: %s, session_id: %s, error: %v",
				eventID, sessionID, err)
		}
	}

	// Only now is it safe to let go of these entries.
	if leaving := slices.Concat(admittedSsIDs, staleSsIDs); len(leaving) > 0 {
		if err := qp.qSvc.RemoveFromQueue(processingCtx, eventID, leaving...); err != nil {
			// Self-correcting: they already hold slots, so the next tick sees
			// them as not-admittable and drops them.
			qp.l.Errorf(processingCtx, "Failed to remove settled sessions from queue - event_id: %s, count: %d, error: %v",
				eventID, len(leaving), err)
		}
	}

	admittedCount := len(admittedSsIDs)

	qp.mu.Lock()
	qp.totalAdmitted += int64(admittedCount)
	qp.mu.Unlock()

	qp.l.Infof(processingCtx, "Batch processing completed - event_id: %s, attempted: %d, admitted: %d",
		eventID, len(ssIDs), admittedCount)

	return nil
}

// admitCount is how many of the buyers due a tick lets in.
//
//	inventory counted the event -> while tickets available outnumber the buyers inside
//	nothing to judge by         -> all of them: the door speed alone paces admission
//
// Each buyer inside counts as one ticket still to take.
// See docs/design/admission-sizing.md#room-size-per-event.
func admitCount(due int, s Stock, inside int64) int {
	if !s.Counted {
		return due
	}
	return int(max(0, min(int64(due), s.Available-inside)))
}

// sweepBatchSize is how many entries one tick may end or drop while the door is shut.
// Each costs a Redis read or two and no checkout, so it need not wait on door speed.
const sweepBatchSize = 100

// closeLine ends a sold-out event's queued sessions, a batch per tick.
//
//	queued        -> sold_out, then leaves the line
//	gone or ended -> leaves the line
//	admitted      -> stays: it holds a token; its checkout gets inventory's answer
//
// Ordering: status before removal, so a failure between leaves an ended entry to remove next tick.
func (qp *queueProcessor) closeLine(ctx context.Context, eventID string) error {
	ssIDs, err := qp.qSvc.PeekQueue(ctx, eventID, sweepBatchSize)
	if err != nil {
		return fmt.Errorf("failed to peek queue: %w", err)
	}

	leaving := make([]string, 0, len(ssIDs))
	for _, id := range ssIDs {
		ss, err := qp.ssSvc.GetSession(ctx, id)
		switch {
		case errors.Is(err, ErrSessionNotFound):
			leaving = append(leaving, id)
		case err != nil:
			qp.l.Errorf(ctx, "Failed to read session while closing the line, leaving it for the next tick - event_id: %s, session_id: %s, error: %v", eventID, id, err)
		case ss.Status == models.SessionStatusQueued:
			if err := qp.ssSvc.UpdateSessionStatus(ctx, id, models.SessionStatusSoldOut); err != nil {
				qp.l.Errorf(ctx, "Failed to end session while closing the line, leaving it for the next tick - event_id: %s, session_id: %s, error: %v", eventID, id, err)
				continue
			}
			leaving = append(leaving, id)
		case ss.IsTerminal():
			leaving = append(leaving, id)
		}
	}

	if len(leaving) == 0 {
		return nil
	}

	if err := qp.qSvc.RemoveFromQueue(ctx, eventID, leaving...); err != nil {
		// Self-correcting: the next tick finds them ended and removes them.
		qp.l.Errorf(ctx, "Failed to remove ended sessions from a sold-out line - event_id: %s, count: %d, error: %v", eventID, len(leaving), err)
		return nil
	}

	qp.l.Infof(ctx, "Closed sold-out line - event_id: %s, sessions: %d", eventID, len(leaving))
	return nil
}

// dropDeadHead removes entries from the front of a paused line whose sessions can
// never be admitted -- gone, ended or expired -- stopping at the first that can.
// Why: a paused door admits nobody, so nothing else drops them, and a line of only
// dead entries would ask inventory every tick. Front first: sessions die in join order.
func (qp *queueProcessor) dropDeadHead(ctx context.Context, eventID string) error {
	ssIDs, err := qp.qSvc.PeekQueue(ctx, eventID, sweepBatchSize)
	if err != nil {
		return fmt.Errorf("failed to peek queue: %w", err)
	}

	dead := make([]string, 0, len(ssIDs))
	for _, id := range ssIDs {
		ss, err := qp.ssSvc.GetSession(ctx, id)
		if errors.Is(err, ErrSessionNotFound) {
			dead = append(dead, id)
			continue
		}
		if err != nil || !(ss.IsTerminal() || ss.IsExpired()) {
			break
		}
		dead = append(dead, id)
	}

	if len(dead) == 0 {
		return nil
	}

	if err := qp.qSvc.RemoveFromQueue(ctx, eventID, dead...); err != nil {
		// Harmless: they are still dead next tick.
		qp.l.Errorf(ctx, "Failed to drop dead entries from a paused line - event_id: %s, count: %d, error: %v", eventID, len(dead), err)
		return nil
	}

	qp.l.Infof(ctx, "Dropped dead entries from a paused line - event_id: %s, count: %d", eventID, len(dead))
	return nil
}

func (qp *queueProcessor) admitUserToCheckout(ctx context.Context, eventID, sessionID string) error {
	return qp.withRetry(ctx, func() error {
		return qp.doAdmitUserToCheckout(ctx, eventID, sessionID)
	})
}

func (qp *queueProcessor) doAdmitUserToCheckout(ctx context.Context, eventID, sessionID string) error {
	ss, err := qp.ssSvc.GetSession(ctx, sessionID)
	if err != nil {
		// The session outlived its TTL, so there is nothing left to admit.
		if errors.Is(err, ErrSessionNotFound) {
			return fmt.Errorf("%w: session not found", ErrSessionNotAdmittable)
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	if !ss.CanAdmit() {
		return qp.resumeOrReject(ctx, eventID, ss)
	}

	token, err := qp.ssSvc.GenerateCheckoutToken(ctx, ss)
	if err != nil {
		return fmt.Errorf("failed to generate checkout token: %w", err)
	}

	expAt := time.Now().Add(qp.cfg.CheckoutTTL)

	if err := qp.ssSvc.UpdateCheckoutToken(ctx, sessionID, token, expAt); err != nil {
		return fmt.Errorf("failed to update session with checkout token: %w", err)
	}

	return qp.claimSlot(ctx, eventID, ss, token, expAt)
}

// resumeOrReject settles a queued session that cannot be admitted the normal way.
//
// half-finished (token committed, slot write did not) -> resume on the stored token
// anything else                                       -> not admittable, drop it
// Why: calling a half-finished admission terminal silently drops the user.
func (qp *queueProcessor) resumeOrReject(ctx context.Context, eventID string, ss *models.Session) error {
	// Left the queue, expired, or never got far enough to have a usable token.
	notAdmittable := fmt.Errorf("%w: status=%s, expired=%v",
		ErrSessionNotAdmittable, ss.Status, ss.IsExpired())

	if ss.Status != models.SessionStatusAdmitted ||
		ss.IsExpired() ||
		ss.HasCheckoutExpired() ||
		ss.CheckoutToken == "" ||
		ss.CheckoutExpiresAt == nil {
		return notAdmittable
	}

	holding, err := qp.qSvc.IsProcessing(ctx, eventID, ss.ID)
	if err != nil {
		// Transient: leave the user queued rather than guess.
		return fmt.Errorf("failed to check checkout slot: %w", err)
	}

	if holding {
		// Genuinely admitted; the queue entry is just stale bookkeeping.
		return notAdmittable
	}

	qp.l.Warnf(ctx, "Resuming half-finished admission - event_id: %s, session_id: %s", eventID, ss.ID)

	return qp.claimSlot(ctx, eventID, ss, ss.CheckoutToken, *ss.CheckoutExpiresAt)
}

// claimSlot takes the checkout slot and announces it.
// Ordering: slot before queue release, so a failure here leaves the user queued.
func (qp *queueProcessor) claimSlot(ctx context.Context, eventID string, ss *models.Session, token string, expAt time.Time) error {
	// No status rollback on failure: the session stays admitted without a slot,
	// which resumeOrReject finishes on a later tick.
	// Why: a rollback that silently fails marks it stale and drops the user.
	if err := qp.qSvc.AddToProcessing(ctx, eventID, ss.ID, time.Until(expAt)); err != nil {
		return fmt.Errorf("failed to add to processing: %w", err)
	}

	evt := kafka.QueueReadyEvent{
		SessionID:     ss.ID,
		UserID:        ss.UserID,
		EventID:       eventID,
		CheckoutToken: token,
		AdmittedAt:    time.Now(),
		ExpiresAt:     expAt,
		Timestamp:     time.Now(),
	}

	if err := qp.prod.PublishQueueReady(ctx, evt); err != nil {
		// The admission is already durable, so a publish failure must not fail
		// the call: that path marks the session not-admittable and drops the
		// user out of the position broadcast. Buffer for the next tick instead.
		qp.l.Errorf(ctx, "Failed to publish QUEUE_READY, buffering for retry - session_id: %s, error: %v",
			ss.ID, err)
		qp.bufferQueueReady(ctx, evt)
	}

	qp.l.Infof(ctx, "User admitted to checkout successfully - session_id: %s, user_id: %s, event_id: %s, expires_at: %v",
		ss.ID, ss.UserID, eventID, expAt)

	return nil
}

// bufferQueueReady parks an unpublished QUEUE_READY event for a later tick.
// Last line of defence: a failure here is the one case where the event is lost.
func (qp *queueProcessor) bufferQueueReady(ctx context.Context, evt kafka.QueueReadyEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		qp.incrementErrorCount()
		qp.l.Errorf(ctx, "QUEUE_READY event LOST, cannot marshal - session_id: %s, error: %v",
			evt.SessionID, err)
		return
	}

	if err := qp.qSvc.BufferQueueReady(ctx, payload); err != nil {
		qp.incrementErrorCount()
		qp.l.Errorf(ctx, "QUEUE_READY event LOST, cannot buffer - session_id: %s, error: %v",
			evt.SessionID, err)
	}
}

// drainBufferedQueueReady republishes events parked by a previous tick.
// Peek, then trim only what published; stop at the first failure to keep order.
func (qp *queueProcessor) drainBufferedQueueReady(ctx context.Context) {
	payloads, err := qp.qSvc.PeekBufferedQueueReady(ctx, qp.cfg.BatchSize)
	if err != nil {
		qp.l.Errorf(ctx, "Failed to read buffered QUEUE_READY events: %v", err)
		return
	}

	if len(payloads) == 0 {
		return
	}

	settled := 0
	for _, payload := range payloads {
		var evt kafka.QueueReadyEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			// Never publishable; drop it rather than wedge the buffer behind it.
			qp.incrementErrorCount()
			qp.l.Errorf(ctx, "Discarding unparseable buffered QUEUE_READY event: %v", err)
			settled++
			continue
		}

		// An expired token would announce a checkout nobody can complete.
		if !evt.ExpiresAt.IsZero() && time.Now().After(evt.ExpiresAt) {
			qp.l.Warnf(ctx, "Discarding expired buffered QUEUE_READY event - session_id: %s, expires_at: %v",
				evt.SessionID, evt.ExpiresAt)
			settled++
			continue
		}

		if err := qp.prod.PublishQueueReady(ctx, evt); err != nil {
			qp.l.Warnf(ctx, "Buffered QUEUE_READY still failing, will retry - session_id: %s, error: %v",
				evt.SessionID, err)
			break
		}

		settled++
	}

	if settled == 0 {
		return
	}

	if err := qp.qSvc.TrimBufferedQueueReady(ctx, settled); err != nil {
		// Already published; a failed trim republishes next tick. Duplicates are safe.
		qp.l.Errorf(ctx, "Failed to trim buffered QUEUE_READY events - settled: %d, error: %v",
			settled, err)
		return
	}

	qp.l.Infof(ctx, "Republished buffered QUEUE_READY events - count: %d", settled)
}

func (qp *queueProcessor) getActiveEvents(ctx context.Context) ([]string, error) {
	activeEvents, err := qp.qSvc.GetActiveEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get active events from Redis: %w", err)
	}

	if len(activeEvents) > 0 {
		qp.l.Debugf(ctx, "Retrieved active events from Redis - count: %d, events: %v",
			len(activeEvents), activeEvents)
	}

	return activeEvents, nil
}

func (qp *queueProcessor) withRetry(ctx context.Context, operation func() error) error {
	var lastErr error

	for attempt := 0; attempt < qp.cfg.RetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(qp.cfg.RetryDelay * time.Duration(attempt)):
				// Linear backoff: RetryDelay * attempt.
			}
		}

		if err := operation(); err != nil {
			lastErr = err

			// Retrying cannot change the outcome, and the caller needs the
			// unwrapped signal to drop the queue entry.
			if errors.Is(err, ErrSessionNotAdmittable) {
				return err
			}

			qp.l.Warnf(ctx, "Operation failed, retrying - attempt: %d/%d, error: %v",
				attempt+1, qp.cfg.RetryAttempts, err)
			continue
		}

		return nil
	}

	return fmt.Errorf("operation failed after %d attempts: %w", qp.cfg.RetryAttempts, lastErr)
}

func (qp *queueProcessor) incrementErrorCount() {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	qp.errorCount++
}

func (qp *queueProcessor) GetStatus() ProcessorStatus {
	qp.mu.RLock()
	defer qp.mu.RUnlock()

	return ProcessorStatus{
		IsRunning:     qp.isRunning,
		StartedAt:     qp.startedAt,
		LastProcessed: qp.lastProcessed,
		TotalAdmitted: qp.totalAdmitted,
		ErrorCount:    qp.errorCount,
	}
}
