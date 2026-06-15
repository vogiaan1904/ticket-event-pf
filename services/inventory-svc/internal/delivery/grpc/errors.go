package grpc

import (
	pkgErrors "github.com/vogiaan/ticketbottle-inventory/pkg/errors"
	"google.golang.org/grpc/codes"
	"gorm.io/gorm"
)

var (
	ErrValidationFailed = pkgErrors.NewGRPCError(codes.InvalidArgument, "validation failed")
)

func (s *grpcService) mapError(err error) error {
	if err == gorm.ErrRecordNotFound {
		return pkgErrors.ErrNotFound
	}
	if err == gorm.ErrInvalidData {
		return pkgErrors.ErrInsufficientStock
	}
	return pkgErrors.ErrInternal
}
