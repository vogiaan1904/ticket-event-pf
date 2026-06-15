package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/event"
)

var ()

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

	ssIDs, err := qp.qSvc.PopFromQueue(processingCtx, eventID, int(batchSize))
	if err != nil {
		return fmt.Errorf("failed to pop from queue: %w", err)
	}

	if len(ssIDs) == 0 {
		qp.l.Debugf(processingCtx, "No sessions to process in queue, event_id: %s", eventID)
		return nil
	}

	qp.l.Infof(processingCtx, "Starting batch admission, event_id: %s, session_count: %d", eventID, len(ssIDs))

	admittedCount := 0
	admittedSsIDs := make([]string, 0, len(ssIDs))

	for _, sessionID := range ssIDs {
		if err := qp.admitUserToCheckout(processingCtx, eventID, sessionID); err != nil {
			qp.l.Errorf(processingCtx, "Failed to admit user to checkout, event_id: %s, session_id: %s", eventID, sessionID)
		} else {
			admittedCount++
			admittedSsIDs = append(admittedSsIDs, sessionID)
		}
	}

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
		return fmt.Errorf("failed to get session: %w", err)
	}

	if !ss.CanAdmit() {
		return fmt.Errorf("session cannot be admitted: status=%s, expired=%v",
			ss.Status, ss.IsExpired())
	}

	token, err := qp.ssSvc.GenerateCheckoutToken(ctx, ss)
	if err != nil {
		return fmt.Errorf("failed to generate checkout token: %w", err)
	}

	expAt := time.Now().Add(15 * time.Minute)

	err = qp.ssSvc.UpdateCheckoutToken(ctx, sessionID, token, expAt)
	if err != nil {
		return fmt.Errorf("failed to update session with checkout token: %w", err)
	}

	err = qp.qSvc.AddToProcessing(ctx, eventID, sessionID, 15*time.Minute)
	if err != nil {
		// Try to rollback session update (best effort)
		qp.ssSvc.UpdateSessionStatus(ctx, sessionID, models.SessionStatusQueued)
		return fmt.Errorf("failed to add to processing: %w", err)
	}

	err = qp.prod.PublishQueueReady(ctx, kafka.QueueReadyEvent{
		SessionID:     sessionID,
		UserID:        ss.UserID,
		EventID:       eventID,
		CheckoutToken: token,
		AdmittedAt:    time.Now(),
		ExpiresAt:     expAt,
		Timestamp:     time.Now(),
	})
	if err != nil {
		qp.l.Errorf(ctx, "Failed to publish QUEUE_READY event - session_id: %s, error: %v",
			sessionID, err)
	}

	qp.l.Infof(ctx, "User admitted to checkout successfully - session_id: %s, user_id: %s, event_id: %s, expires_at: %v",
		sessionID, ss.UserID, eventID, expAt)

	return nil
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

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
