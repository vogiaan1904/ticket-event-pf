package grpc

import (
	"errors"

	svc "github.com/vogiaan/ticketbottle-inventory/internal/services"
	pkgErrors "github.com/vogiaan/ticketbottle-inventory/pkg/errors"
	"google.golang.org/grpc/codes"
	"gorm.io/gorm"
)

var (
	ErrValidationFailed = pkgErrors.NewGRPCError(codes.InvalidArgument, "validation failed")
)

func (s *grpcService) mapError(err error) error {
	switch {
	case errors.Is(err, svc.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return pkgErrors.ErrNotFound
	case errors.Is(err, svc.ErrInsufficientStock):
		return pkgErrors.ErrInsufficientStock
	case errors.Is(err, svc.ErrStateConflict):
		return pkgErrors.ErrConflict
	case errors.Is(err, svc.ErrInventoryDrift):
		return pkgErrors.ErrInternal
	default:
		return pkgErrors.ErrInternal
	}
}
