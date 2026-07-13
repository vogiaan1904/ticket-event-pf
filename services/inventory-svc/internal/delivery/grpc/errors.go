package grpc

import (
	svc "github.com/vogiaan/ticketbottle-inventory/internal/services"
	pkgErrors "github.com/vogiaan/ticketbottle-inventory/pkg/errors"
	"google.golang.org/grpc/codes"
	"gorm.io/gorm"
)

var (
	ErrValidationFailed = pkgErrors.NewGRPCError(codes.InvalidArgument, "validation failed")
)

func (s *grpcService) mapError(err error) error {
	switch err {
	case gorm.ErrRecordNotFound:
		return pkgErrors.ErrNotFound
	case gorm.ErrInvalidData:
		return pkgErrors.ErrInsufficientStock
	case svc.ErrStateConflict:
		return pkgErrors.ErrConflict
	default:
		return pkgErrors.ErrInternal
	}
}
