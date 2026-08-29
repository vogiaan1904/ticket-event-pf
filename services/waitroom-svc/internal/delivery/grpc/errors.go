package grpc

import (
	"errors"

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

// Matched with errors.Is, not ==: a wrapped sentinel would otherwise fall to
// default, which ParseGRPCError renders as codes.Internal — a 500 for what is
// usually an ordinary rejection.
func (svc *grpcService) mapGRPCError(err error) error {
	switch {
	case errors.Is(err, service.ErrSessionNotFound):
		return ErrSessionNotFound
	case errors.Is(err, service.ErrSessionExpired):
		return ErrSessionExpired
	case errors.Is(err, service.ErrSessionAlreadyExists):
		return ErrSessionAlreadyExists
	case errors.Is(err, service.ErrInvalidSessionStatus):
		return ErrInvalidSessionStatus
	case errors.Is(err, service.ErrQueueFull):
		return ErrQueueFull
	case errors.Is(err, service.ErrEventNotFound):
		return ErrEventNotFound
	case errors.Is(err, service.ErrEventConfigNotFound):
		return ErrEventConfigNotFound
	case errors.Is(err, service.ErrWaitRoomNotAllowed):
		return ErrWaitRoomNotAllowed
	default:
		return err
	}
}
