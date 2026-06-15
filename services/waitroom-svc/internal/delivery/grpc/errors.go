package grpc

import (
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	pkgErrors "github.com/vogiaan1904/ticketbottle-waitroom/pkg/errors"
)

var (
	ErrSessionNotFound      = pkgErrors.NewGRPCError("WTR001", "Session not found")
	ErrSessionExpired       = pkgErrors.NewGRPCError("WTR002", "Session expired")
	ErrSessionAlreadyExists = pkgErrors.NewGRPCError("WTR003", "Session already exists")
	ErrInvalidSessionStatus = pkgErrors.NewGRPCError("WTR004", "Invalid session status")

	ErrQueueFull           = pkgErrors.NewGRPCError("WTR005", "Queue is full")
	ErrEventNotFound       = pkgErrors.NewGRPCError("WTR006", "Event not found")
	ErrEventConfigNotFound = pkgErrors.NewGRPCError("WTR008", "Event config not found")
	ErrWaitRoomNotAllowed  = pkgErrors.NewGRPCError("WTR009", "Wait room is not allowed")
)

func (svc *grpcService) mapGRPCError(err error) error {
	switch err {
	case service.ErrSessionNotFound:
		return ErrSessionNotFound
	case service.ErrSessionExpired:
		return ErrSessionExpired
	case service.ErrSessionAlreadyExists:
		return ErrSessionAlreadyExists
	case service.ErrInvalidSessionStatus:
		return ErrInvalidSessionStatus
	case service.ErrQueueFull:
		return ErrQueueFull
	case service.ErrEventNotFound:
		return ErrEventNotFound
	case service.ErrEventConfigNotFound:
		return ErrEventConfigNotFound
	case service.ErrWaitRoomNotAllowed:
		return ErrWaitRoomNotAllowed
	default:
		return err
	}
}
