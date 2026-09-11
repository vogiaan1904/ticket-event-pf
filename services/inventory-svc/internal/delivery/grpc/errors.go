package grpc

import (
	"errors"

	"github.com/vogiaan/ticketbottle-inventory/internal/metrics"
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
	case errors.Is(err, svc.ErrSaleClosed):
		return pkgErrors.ErrSaleClosed
	case errors.Is(err, svc.ErrStateConflict):
		return pkgErrors.ErrConflict
	case errors.Is(err, svc.ErrInventoryDrift):
		return pkgErrors.ErrInternal
	default:
		return pkgErrors.ErrInternal
	}
}

// reserveResult classifies a failed Reserve for tb_inventory_reserve_total.
// Only insufficient stock is sold_out; a closed sale or a state conflict is not
// the show selling out, and the `code` label already separates those.
func reserveResult(err error) string {
	if errors.Is(err, svc.ErrInsufficientStock) {
		return metrics.ReserveSoldOut
	}
	return metrics.ReserveError
}
