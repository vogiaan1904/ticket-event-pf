package grpc

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	resp "github.com/vogiaan1904/ticketbottle-waitroom/pkg/response"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/util"
	waitroompb "github.com/vogiaan1904/ticketbottle-waitroom/protogen/waitroom"
)

type grpcService struct {
	svc service.WaitroomService
	l   logger.Logger
	waitroompb.UnimplementedWaitroomServiceServer
}

func NewGrpcService(svc service.WaitroomService, l logger.Logger) waitroompb.WaitroomServiceServer {
	return &grpcService{
		svc: svc,
		l:   l,
	}
}

func (s *grpcService) JoinQueue(ctx context.Context, req *waitroompb.JoinQueueRequest) (*waitroompb.JoinQueueResponse, error) {
	input := service.JoinQueueInput{
		UserID:    req.UserId,
		EventID:   req.EventId,
		UserAgent: req.UserAgent,
		IPAddress: req.IpAddress,
	}

	out, err := s.svc.JoinQueue(ctx, &input)
	if err != nil {
		s.l.Errorf(ctx, "Failed to join queue: %v", err)
		err = s.mapGRPCError(err)
		return nil, resp.ParseGRPCError(err)
	}

	return &waitroompb.JoinQueueResponse{
		SessionId:    out.SessionID,
		Position:     out.Position,
		QueueLength:  out.QueueLength,
		QueuedAt:     util.TimeToISO8601Str(out.QueuedAt),
		ExpiresAt:    util.TimeToISO8601Str(out.ExpiresAt),
		WebsocketUrl: out.WebSocketURL,
	}, nil
}

func (s *grpcService) GetQueueStatus(ctx context.Context, req *waitroompb.GetQueueStatusRequest) (*waitroompb.QueueStatusResponse, error) {
	out, err := s.svc.GetQueueStatus(ctx, req.SessionId)
	if err != nil {
		s.l.Errorf(ctx, "Failed to get queue status: %v", err)
		err = s.mapGRPCError(err)
		return nil, resp.ParseGRPCError(err)
	}

	return &waitroompb.QueueStatusResponse{
		SessionId:     out.SessionID,
		Position:      out.Position,
		QueueLength:   out.QueueLength,
		QueuedAt:      util.TimeToISO8601Str(out.QueuedAt),
		ExpiresAt:     util.TimeToISO8601Str(out.ExpiresAt),
		CheckoutToken: out.CheckoutToken,
		CheckoutUrl:   out.CheckoutURL,
	}, nil
}

func (s *grpcService) LeaveQueue(ctx context.Context, req *waitroompb.LeaveQueueRequest) (*waitroompb.LeaveQueueResponse, error) {
	err := s.svc.LeaveQueue(ctx, req.SessionId)
	if err != nil {
		return nil, resp.ParseGRPCError(err)
	}

	return &waitroompb.LeaveQueueResponse{
		SessionId: req.SessionId,
		Message:   "Queue left successfully",
	}, nil
}

func (s *grpcService) StreamQueuePosition(req *waitroompb.StreamPositionRequest, stream waitroompb.WaitroomService_StreamQueuePositionServer) error {
	ctx := stream.Context()

	s.l.Info(ctx, "Starting position stream",
		"session_id", req.SessionId,
	)

	upds := make(chan *service.PositionStreamUpdate, 10)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.svc.StreamSessionPosition(ctx, req.SessionId, upds)
	}()

	for {
		select {
		case <-ctx.Done():
			s.l.Info(ctx, "Position stream cancelled by client",
				"session_id", req.SessionId,
			)
			return ctx.Err()

		case err := <-errCh:
			if err != nil {
				s.l.Error(ctx, "Position stream error",
					"session_id", req.SessionId,
					"error", err,
				)
				return resp.ParseGRPCError(err)
			}
			s.l.Info(ctx, "Position stream completed",
				"session_id", req.SessionId,
			)
			return nil

		case upd := <-upds:
			if upd == nil {
				return nil
			}

			pbUpd := &waitroompb.PositionUpdate{
				SessionId:     upd.SessionID,
				Position:      upd.Position,
				QueueLength:   upd.QueueLength,
				Status:        convertSessionStatusToProto(upd.Status),
				UpdatedAt:     util.TimeToISO8601Str(upd.UpdatedAt),
				CheckoutToken: upd.CheckoutToken,
				CheckoutUrl:   upd.CheckoutURL,
			}

			if err := stream.Send(pbUpd); err != nil {
				s.l.Error(ctx, "Failed to send position update",
					"session_id", req.SessionId,
					"error", err,
				)
				return err
			}

			s.l.Debug(ctx, "Sent position update to client",
				"session_id", req.SessionId,
				"position", upd.Position,
				"status", upd.Status,
			)
		}
	}
}

func convertSessionStatusToProto(status models.SessionStatus) waitroompb.SessionStatus {
	switch status {
	case models.SessionStatusQueued:
		return waitroompb.SessionStatus_SESSION_STATUS_QUEUED
	case models.SessionStatusAdmitted:
		return waitroompb.SessionStatus_SESSION_STATUS_READY
	case models.SessionStatusCompleted:
		return waitroompb.SessionStatus_SESSION_STATUS_COMPLETED
	case models.SessionStatusExpired:
		return waitroompb.SessionStatus_SESSION_STATUS_EXPIRED
	case models.SessionStatusFailed:
		return waitroompb.SessionStatus_SESSION_STATUS_FAILED
	case models.SessionStatusAbandoned:
		return waitroompb.SessionStatus_SESSION_STATUS_CANCELLED
	default:
		return waitroompb.SessionStatus_SESSION_STATUS_UNSPECIFIED
	}
}
