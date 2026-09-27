package service

import (
	"context"
	"fmt"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	pkgLog "github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/event"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// eventServiceError classifies a failed call to event-svc.
// NOT_FOUND -> a verdict about the event; anything else -> the dependency failed.
func eventServiceError(err error, notFound error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return notFound
	case codes.DeadlineExceeded, codes.Canceled:
		return ErrEventServiceTimeout
	default:
		return ErrEventServiceUnavailable
	}
}

type WaitroomService interface {
	JoinQueue(ctx context.Context, req *JoinQueueInput) (*JoinQueueOutput, error)
	GetQueueStatus(ctx context.Context, ssID, userID string) (*QueueStatusOutput, error)
	LeaveQueue(ctx context.Context, ssID, userID string) error
	HandleCheckoutCompleted(ctx context.Context, in CheckoutCompletedInput) error
	HandleCheckoutFailed(ctx context.Context, in CheckoutFailedInput) error
	HandleCheckoutExpired(ctx context.Context, in CheckoutExpiredInput) error

	StartQueueProcessor(ctx context.Context) error
	StopQueueProcessor() error
	GetProcessorStatus() ProcessorStatus
}

type waitroomService struct {
	qSvc  QueueService
	ssSvc SessionService
	eGate *eventGate
	prod  producer.Producer
	l     pkgLog.Logger
	proc  QueueProcessor
}

func NewWaitroomService(
	qSvc QueueService,
	ssSvc SessionService,
	eSvc event.EventServiceClient,
	prod producer.Producer,
	l pkgLog.Logger,
	proc QueueProcessor,
	eventCacheTTL time.Duration,
) WaitroomService {
	return &waitroomService{
		qSvc:  qSvc,
		ssSvc: ssSvc,
		eGate: newEventGate(eSvc, eventCacheTTL),
		prod:  prod,
		l:     l,
		proc:  proc,
	}
}

func (s *waitroomService) JoinQueue(ctx context.Context, in *JoinQueueInput) (*JoinQueueOutput, error) {
	eInfo, err := s.eGate.Get(ctx, in.EventID)
	if err != nil {
		s.l.Errorf(ctx, "service.waitroomService.JoinQueue: %v", err)
		return nil, err
	}

	if !eInfo.AllowWaitRoom {
		s.l.Warnf(ctx, "service.waitroomService.JoinQueue: %v", ErrWaitRoomNotAllowed)
		return nil, ErrWaitRoomNotAllowed
	}

	ss, err := s.ssSvc.CreateSession(
		ctx,
		in.UserID,
		in.EventID,
		in.UserAgent,
		in.IPAddress,
		eInfo.SaleStartAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	pos, err := s.qSvc.EnqueueSession(ctx, ss)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue session: %w", err)
	}

	if err := s.prod.PublishQueueJoined(ctx, kafka.QueueJoinedEvent{
		SessionID: ss.ID,
		UserID:    ss.UserID,
		EventID:   ss.EventID,
		Position:  pos,
		JoinedAt:  ss.QueuedAt,
	}); err != nil {
		s.l.Errorf(ctx, "service.waitroomService.JoinQueue: %v", err)
	}

	qInf, err := s.qSvc.GetQueueInfo(ctx, in.EventID)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue info: %w", err)
	}

	return &JoinQueueOutput{
		SessionID:   ss.ID,
		Position:    pos,
		QueueLength: qInf.QueueLength,
		QueuedAt:    ss.QueuedAt,
		ExpiresAt:   ss.ExpiresAt,
	}, nil
}

func (s *waitroomService) GetQueueStatus(ctx context.Context, ssID, userID string) (*QueueStatusOutput, error) {
	ss, err := s.ssSvc.ActiveSession(ctx, ssID, userID)
	if err != nil {
		return nil, err
	}

	stt, err := s.qSvc.GetQueueStatus(ctx, ssID, ss)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue status: %w", err)
	}

	return stt, nil
}

func (s *waitroomService) LeaveQueue(ctx context.Context, ssID, userID string) error {
	ss, err := s.ssSvc.GetSession(ctx, ssID)
	if err != nil {
		s.l.Errorf(ctx, "waitroomService.LeaveQueue: %v", err)
		return err
	}
	if ss.UserID != userID {
		return ErrSessionNotFound
	}

	if err := s.qSvc.DequeueSession(ctx, ss.EventID, ssID); err != nil {
		return fmt.Errorf("failed to leave queue: %w", err)
	}

	if err := s.ssSvc.UpdateSessionStatus(ctx, ssID, models.SessionStatusAbandoned); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	if err := s.prod.PublishQueueLeft(ctx, kafka.QueueLeftEvent{
		SessionID: ssID,
		UserID:    ss.UserID,
		EventID:   ss.EventID,
		Reason:    "user_left",
		LeftAt:    ss.UpdatedAt,
	}); err != nil {
		s.l.Errorf(ctx, "Failed to publish queue left event - session_id: %s, error: %v", ssID, err)
	}

	s.l.Infof(ctx, "User left queue - session_id: %s, user_id: %s, event_id: %s",
		ssID, ss.UserID, ss.EventID)

	return nil
}

func (s *waitroomService) HandleCheckoutCompleted(ctx context.Context, in CheckoutCompletedInput) error {
	ss, err := s.ssSvc.GetSession(ctx, in.SessionID)
	if err != nil {
		if err == ErrSessionNotFound {
			s.l.Warnf(ctx, "Session not found for completed checkout",
				"session_id", in.SessionID,
			)
			return nil
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	if err := s.ssSvc.InvalidateCheckoutToken(ctx, in.SessionID, "completed"); err != nil {
		s.l.Errorf(ctx, "Failed to invalidate checkout token - session_id: %s, error: %v",
			in.SessionID, err)
	}

	if err := s.qSvc.RemoveFromProcessing(ctx, in.EventID, in.SessionID); err != nil {
		s.l.Errorf(ctx, "Failed to remove from processing - session_id: %s, error: %v",
			in.SessionID, err)
	}

	ss.Status = models.SessionStatusCompleted
	now := in.Timestamp
	ss.CompletedAt = &now

	if err := s.ssSvc.UpdateSession(ctx, ss); err != nil {
		s.l.Errorf(ctx, "Failed to update session - session_id: %s, error: %v",
			in.SessionID, err)
		return err
	}

	return nil
}

func (s *waitroomService) HandleCheckoutFailed(ctx context.Context, in CheckoutFailedInput) error {
	ss, err := s.ssSvc.GetSession(ctx, in.SessionID)
	if err != nil {
		if err == ErrSessionNotFound {
			s.l.Warnf(ctx, "Session not found for failed checkout - session_id: %s", in.SessionID)
			return nil
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	if err := s.ssSvc.InvalidateCheckoutToken(ctx, in.SessionID, "failed"); err != nil {
		s.l.Errorf(ctx, "Failed to invalidate checkout token - session_id: %s, error: %v",
			in.SessionID, err)
	}

	if err := s.qSvc.RemoveFromProcessing(ctx, in.EventID, in.SessionID); err != nil {
		s.l.Errorf(ctx, "Failed to remove from processing - session_id: %s, error: %v",
			in.SessionID, err)
	}

	ss.Status = models.SessionStatusFailed
	if err := s.ssSvc.UpdateSession(ctx, ss); err != nil {
		s.l.Errorf(ctx, "Failed to update session - session_id: %s, error: %v",
			in.SessionID, err)
		return err
	}

	return nil
}

func (s *waitroomService) HandleCheckoutExpired(ctx context.Context, in CheckoutExpiredInput) error {
	ss, err := s.ssSvc.GetSession(ctx, in.SessionID)
	if err != nil {
		if err == ErrSessionNotFound {
			s.l.Warnf(ctx, "Session not found for expired checkout - session_id: %s", in.SessionID)
			return nil
		}
		s.l.Errorf(ctx, "Failed to get session - session_id: %s, error: %v",
			in.SessionID, err)
		return err
	}

	if err := s.ssSvc.InvalidateCheckoutToken(ctx, in.SessionID, "expired"); err != nil {
		s.l.Errorf(ctx, "Failed to invalidate checkout token - session_id: %s, error: %v",
			in.SessionID, err)
	}

	if err := s.qSvc.RemoveFromProcessing(ctx, in.EventID, in.SessionID); err != nil {
		s.l.Errorf(ctx, "Failed to remove from processing - session_id: %s, error: %v",
			in.SessionID, err)
	}

	ss.Status = models.SessionStatusExpired
	if err := s.ssSvc.UpdateSession(ctx, ss); err != nil {
		s.l.Errorf(ctx, "Failed to update session - session_id: %s, error: %v",
			in.SessionID, err)
		return err
	}

	return nil
}

func (s *waitroomService) StartQueueProcessor(ctx context.Context) error {
	if s.proc == nil {
		return fmt.Errorf("queue processor not initialized")
	}
	return s.proc.Start(ctx)
}

func (s *waitroomService) StopQueueProcessor() error {
	if s.proc == nil {
		return fmt.Errorf("queue processor not initialized")
	}
	return s.proc.Stop()
}

func (s *waitroomService) GetProcessorStatus() ProcessorStatus {
	if s.proc == nil {
		return ProcessorStatus{IsRunning: false}
	}
	return s.proc.GetStatus()
}
