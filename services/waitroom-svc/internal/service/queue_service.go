package service

import (
	"context"
	"fmt"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type QueueService interface {
	EnqueueSession(ctx context.Context, session *models.Session) (int64, error)
	DequeueSession(ctx context.Context, eventID, sessionID string) error
	GetQueueStatus(ctx context.Context, sessionID string, session *models.Session) (*QueueStatusOutput, error)
	GetQueueInfo(ctx context.Context, eventID string) (*QueueInfoOutput, error)
	RemoveFromProcessing(ctx context.Context, eventID, sessionID string) error
	GetProcessingCount(ctx context.Context, eventID string) (int64, error)
	IsProcessing(ctx context.Context, eventID, sessionID string) (bool, error)
	PeekQueue(ctx context.Context, eventID string, count int) ([]string, error)
	RemoveFromQueue(ctx context.Context, eventID string, sessionIDs ...string) error
	AddToProcessing(ctx context.Context, eventID, sessionID string, ttl time.Duration) error
	GetActiveEvents(ctx context.Context) ([]string, error)

	BufferQueueReady(ctx context.Context, payload []byte) error
	PeekBufferedQueueReady(ctx context.Context, count int) ([]string, error)
	TrimBufferedQueueReady(ctx context.Context, count int) error
}

type queueService struct {
	repo repo.QueueRepository
	l    logger.Logger
}

func NewQueueService(
	repo repo.QueueRepository,
	l logger.Logger,
) QueueService {
	return &queueService{
		repo: repo,
		l:    l,
	}
}

func (s *queueService) EnqueueSession(ctx context.Context, ss *models.Session) (int64, error) {
	if err := s.repo.AddToQueue(ctx, ss.EventID, ss); err != nil {
		return 0, fmt.Errorf("failed to add to queue: %w", err)
	}

	pos, err := s.repo.GetQueuePosition(ctx, ss.EventID, ss.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to get queue position: %w", err)
	}

	ss.Position = pos

	if err := s.repo.AddActiveEvent(ctx, ss.EventID); err != nil {
		s.l.Warnf(ctx, "Failed to mark event as active: %v", err)
	}

	s.l.Infof(ctx, "Session enqueued - id: %s, event_id: %s, position: %d", ss.ID, ss.EventID, pos)

	return pos, nil
}

func (s *queueService) DequeueSession(ctx context.Context, eventID, sessionID string) error {
	// Remove from queue
	if err := s.repo.RemoveFromQueue(ctx, eventID, sessionID); err != nil {
		return fmt.Errorf("failed to remove from queue: %w", err)
	}

	s.l.Infof(ctx, "Session dequeued - session_id: %s, event_id: %s", sessionID, eventID)

	// Check if queue is now empty and remove from active events if so
	queueLength, err := s.repo.GetQueueLength(ctx, eventID)
	if err != nil {
		s.l.Warnf(ctx, "Failed to check queue length after dequeue - event_id: %s, error: %v", eventID, err)
	} else if queueLength == 0 {
		// Queue is empty, remove from active events
		if err := s.repo.RemoveActiveEvent(ctx, eventID); err != nil {
			s.l.Warnf(ctx, "Failed to remove event from active set - event_id: %s, error: %v", eventID, err)
		} else {
			s.l.Infof(ctx, "Event queue is empty, removed from active events - event_id: %s", eventID)
		}
	}

	return nil
}

func (s *queueService) GetQueueStatus(ctx context.Context, sessionID string, session *models.Session) (*QueueStatusOutput, error) {
	out := &QueueStatusOutput{
		SessionID: session.ID,
		Status:    session.Status,
		QueuedAt:  session.QueuedAt,
		ExpiresAt: session.ExpiresAt,
	}

	if session.Status == models.SessionStatusQueued {
		position, err := s.repo.GetQueuePosition(ctx, session.EventID, session.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get queue position: %w", err)
		}

		queueLength, err := s.repo.GetQueueLength(ctx, session.EventID)
		if err != nil {
			return nil, fmt.Errorf("failed to get queue length: %w", err)
		}

		out.Position = position
		out.QueueLength = queueLength
	}

	// If admitted, include checkout information
	if session.Status == models.SessionStatusAdmitted {
		out.CheckoutToken = session.CheckoutToken
		out.CheckoutExpiresAt = session.CheckoutExpiresAt
		out.AdmittedAt = session.AdmittedAt
		out.CheckoutURL = "/checkout" // Relative: resolved against the client's own origin.
	}

	return out, nil
}

func (s *queueService) RemoveFromProcessing(ctx context.Context, eventID, sessionID string) error {
	if err := s.repo.RemoveFromProcessing(ctx, eventID, sessionID); err != nil {
		return fmt.Errorf("failed to remove from processing: %w", err)
	}
	return nil
}

func (s *queueService) GetProcessingCount(ctx context.Context, eventID string) (int64, error) {
	return s.repo.GetProcessingCount(ctx, eventID)
}

// IsProcessing reports whether the session currently holds a checkout slot.
func (s *queueService) IsProcessing(ctx context.Context, eventID, sessionID string) (bool, error) {
	return s.repo.IsProcessing(ctx, eventID, sessionID)
}

func (s *queueService) GetQueueInfo(ctx context.Context, eID string) (*QueueInfoOutput, error) {
	qLen, err := s.repo.GetQueueLength(ctx, eID)
	if err != nil {
		return nil, err
	}

	processingCount, err := s.repo.GetProcessingCount(ctx, eID)
	if err != nil {
		return nil, err
	}

	return &QueueInfoOutput{
		EventID:         eID,
		QueueLength:     qLen,
		ProcessingCount: processingCount,
	}, nil
}

// PeekQueue reads the head of the queue without removing anything.
// Entries leave only on a terminal outcome -- see queueProcessor.ProcessEventQueue.
func (s *queueService) PeekQueue(ctx context.Context, eventID string, count int) ([]string, error) {
	if count <= 0 {
		return nil, nil
	}

	sessionIDs, err := s.repo.GetQueueMembers(ctx, eventID, 0, int64(count-1))
	if err != nil {
		return nil, fmt.Errorf("failed to peek queue: %w", err)
	}
	return sessionIDs, nil
}

// RemoveFromQueue drops entries from the queue without the bookkeeping
// DequeueSession does; the caller owns any position broadcast.
func (s *queueService) RemoveFromQueue(ctx context.Context, eventID string, sessionIDs ...string) error {
	if err := s.repo.RemoveFromQueue(ctx, eventID, sessionIDs...); err != nil {
		return fmt.Errorf("failed to remove from queue: %w", err)
	}
	return nil
}

func (s *queueService) AddToProcessing(ctx context.Context, eventID, sessionID string, ttl time.Duration) error {
	if err := s.repo.AddToProcessing(ctx, eventID, sessionID, ttl); err != nil {
		return fmt.Errorf("failed to add to processing: %w", err)
	}
	return nil
}

func (s *queueService) GetActiveEvents(ctx context.Context) ([]string, error) {
	events, err := s.repo.GetActiveEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get active events: %w", err)
	}
	return events, nil
}

func (s *queueService) BufferQueueReady(ctx context.Context, payload []byte) error {
	return s.repo.BufferQueueReady(ctx, payload)
}

func (s *queueService) PeekBufferedQueueReady(ctx context.Context, count int) ([]string, error) {
	return s.repo.PeekBufferedQueueReady(ctx, count)
}

func (s *queueService) TrimBufferedQueueReady(ctx context.Context, count int) error {
	return s.repo.TrimBufferedQueueReady(ctx, count)
}
