package grpc

import (
	"errors"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	pkgErrors "github.com/vogiaan1904/ticketbottle-waitroom/pkg/errors"
	"google.golang.org/grpc/codes"
)

var (
	ErrSessionNotFound      = pkgErrors.NewGRPCError(codes.NotFound, "WTR001", "Session not found")
	ErrSessionExpired       = pkgErrors.NewGRPCError(codes.FailedPrecondition, "WTR002", "Session expired")
	ErrSessionAlreadyExists = pkgErrors.NewGRPCError(codes.AlreadyExists, "WTR003", "Session already exists")
	ErrInvalidSessionStatus = pkgErrors.NewGRPCError(codes.FailedPrecondition, "WTR004", "Invalid session status")

	ErrQueueFull           = pkgErrors.NewGRPCError(codes.FailedPrecondition, "WTR005", "Queue is full")
	ErrEventNotFound       = pkgErrors.NewGRPCError(codes.NotFound, "WTR006", "Event not found")
	ErrEventConfigNotFound = pkgErrors.NewGRPCError(codes.NotFound, "WTR008", "Event config not found")
	ErrWaitRoomNotAllowed  = pkgErrors.NewGRPCError(codes.FailedPrecondition, "WTR009", "Wait room is not allowed")

	ErrEventServiceUnavailable = pkgErrors.NewGRPCError(codes.Unavailable, "WTR010", "Event service unavailable")
	ErrEventServiceTimeout     = pkgErrors.NewGRPCError(codes.DeadlineExceeded, "WTR011", "Event service timed out")
)

// mapGRPCError turns a service error into its wire equivalent. Matching is by
// errors.Is, not ==, so a wrapped sentinel still resolves; anything reaching
// default is rendered as codes.Internal by ParseGRPCError.
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
	case errors.Is(err, service.ErrEventServiceUnavailable):
		return ErrEventServiceUnavailable
	case errors.Is(err, service.ErrEventServiceTimeout):
		return ErrEventServiceTimeout
	default:
		return err
	}
}
