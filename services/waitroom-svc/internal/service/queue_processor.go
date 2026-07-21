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
	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/event"
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
	eSvc          event.EventServiceClient
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
	MaxConcurrentPerEvent int           // Max users in checkout per event
	BatchSize             int           // Max users to admit per batch
	EventCacheTTL         time.Duration // How long to cache active events
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
	eSvc event.EventServiceClient,
	prod producer.Producer,
	l logger.Logger,
	cfg config.QueueConfig,
	jwtCfg config.JWTConfig,
) QueueProcessor {
	return &queueProcessor{
		qSvc:  qSvc,
		ssSvc: ssSvc,
		eSvc:  eSvc,
		prod:  prod,
		l:     l,
		cfg: ProcessorConfig{
			ProcessInterval:       cfg.ProcessInterval,
			MaxConcurrentPerEvent: cfg.DefaultMaxConcurrent,
			BatchSize:             cfg.DefaultReleaseRate,
			EventCacheTTL:         5 * time.Minute,
			RetryAttempts:         3,
			RetryDelay:            time.Second,
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

	qp.l.Infof(ctx, "Starting queue processor - interval: %v, max_concurrent: %d, batch_size: %d",
		qp.cfg.ProcessInterval, qp.cfg.MaxConcurrentPerEvent, qp.cfg.BatchSize)

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

	processingCount, err := qp.qSvc.GetProcessingCount(processingCtx, eventID)
	if err != nil {
		return fmt.Errorf("failed to get processing count: %w", err)
	}

	availableSlots := int64(qp.cfg.MaxConcurrentPerEvent) - processingCount
	if availableSlots <= 0 {
		qp.l.Debugf(processingCtx, "No available slots for event, event_id: %s, processing_count: %d, max_concurrent: %d", eventID, processingCount, qp.cfg.MaxConcurrentPerEvent)
		return nil
	}

	batchSize := min(availableSlots, int64(qp.cfg.BatchSize))

	// Claim/ack rather than pop. An entry leaves the queue only once admission
	// has reached a terminal outcome, so a transient failure retries the same
	// user at the same position on the next tick instead of dropping them.
	// There is deliberately no window in which a session sits in neither the
	// queue nor the processing set.
	ssIDs, err := qp.qSvc.PeekQueue(processingCtx, eventID, int(batchSize))
	if err != nil {
		return fmt.Errorf("failed to peek queue: %w", err)
	}

	if len(ssIDs) == 0 {
		qp.l.Debugf(processingCtx, "No sessions to process in queue, event_id: %s", eventID)
		return nil
	}

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
			// Transient. Leave the entry in the queue; the next tick retries it
			// from the same position.
			qp.l.Errorf(processingCtx, "Failed to admit user, leaving queued for retry - event_id: %s, session_id: %s, error: %v",
				eventID, sessionID, err)
		}
	}

	// Only now is it safe to let go of these entries.
	if leaving := slices.Concat(admittedSsIDs, staleSsIDs); len(leaving) > 0 {
		if err := qp.qSvc.RemoveFromQueue(processingCtx, eventID, leaving...); err != nil {
			// The sessions are already admitted and hold their slots. Leaving
			// them queued is self-correcting: the next tick sees them as
			// not-admittable and drops them then.
			qp.l.Errorf(processingCtx, "Failed to remove settled sessions from queue - event_id: %s, count: %d, error: %v",
				eventID, len(leaving), err)
		}
	}

	admittedCount := len(admittedSsIDs)

	qp.mu.Lock()
	qp.totalAdmitted += int64(admittedCount)
	qp.mu.Unlock()

	if len(admittedSsIDs) > 0 {
		if err := qp.qSvc.PublishPositionUpdate(processingCtx, &models.PositionUpdateEvent{
			EventID:            eventID,
			UpdateType:         models.UpdateTypeUserAdmitted,
			AffectedSessionIDs: admittedSsIDs,
			Timestamp:          time.Now(),
		}); err != nil {
			qp.l.Warnf(processingCtx, "Failed to publish position update after batch admission - event_id: %s, admitted_count: %d, error: %v",
				eventID, len(admittedSsIDs), err)
		}
	}

	qp.l.Infof(processingCtx, "Batch processing completed - event_id: %s, attempted: %d, admitted: %d",
		eventID, len(ssIDs), admittedCount)

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

// resumeOrReject handles a session the queue still lists but that cannot be
// admitted the normal way.
//
// The case that matters is a *half-finished* admission: UpdateCheckoutToken
// committed -- so the session already reads as admitted -- but the slot write
// did not. The session then holds a token and no slot, which is neither state
// the queue understands. Calling that terminal would drop the user, which is
// the exact silent loss this processor exists to prevent, so it is finished
// instead, reusing the token already persisted.
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

// claimSlot takes the checkout slot and announces it. The slot is taken before
// the queue entry is released (see ProcessEventQueue), so any failure here
// leaves the user queued for another attempt.
func (qp *queueProcessor) claimSlot(ctx context.Context, eventID string, ss *models.Session, token string, expAt time.Time) error {
	// Deliberately no status rollback on failure. The session is left admitted
	// without a slot, which resumeOrReject recognises and finishes on a later
	// tick. The previous best-effort rollback discarded its own error, and a
	// rollback that silently failed left the session admitted, un-slotted, and
	// classified as stale -- which dropped the user.
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
		// The admission itself is already durable -- the session is admitted
		// and holds a slot -- so this must not fail the operation. Reporting
		// failure here would send the caller down the not-admittable path and
		// drop the user out of the position broadcast, which is the channel
		// they actually learn about their token on. Buffer the event for the
		// next tick instead of losing it.
		qp.l.Errorf(ctx, "Failed to publish QUEUE_READY, buffering for retry - session_id: %s, error: %v",
			ss.ID, err)
		qp.bufferQueueReady(ctx, evt)
	}

	qp.l.Infof(ctx, "User admitted to checkout successfully - session_id: %s, user_id: %s, event_id: %s, expires_at: %v",
		ss.ID, ss.UserID, eventID, expAt)

	return nil
}

// bufferQueueReady parks an unpublished QUEUE_READY event for a later tick. It
// is the last line of defence, so a failure here is the one case where the event
// is genuinely lost -- say so loudly.
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

// drainBufferedQueueReady republishes events parked by a previous tick. Entries
// are peeked and only trimmed once actually published, so a still-broken broker
// costs a retry rather than the event. It stops at the first failure to keep the
// buffer in order.
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
			// Nothing will ever make this publishable; drop it rather than
			// wedge the buffer behind it.
			qp.incrementErrorCount()
			qp.l.Errorf(ctx, "Discarding unparseable buffered QUEUE_READY event: %v", err)
			settled++
			continue
		}

		// After a long outage the token in here may already have expired;
		// republishing it would announce a checkout nobody can complete.
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
		// The events were published; failing to trim means they republish next
		// tick. Duplicates are the safe direction here.
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
				// Exponential backoff
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

		return nil // Success
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
