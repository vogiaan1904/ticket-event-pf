package order

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
)

type Service interface {
	Create(ctx context.Context, in CreateOrderInput) (CreateOrderOutput, error)
	Cancel(ctx context.Context, code string) error
	GetByID(ctx context.Context, code string) (models.Order, error)
	GetOne(ctx context.Context, in GetOneOrderInput) (models.Order, error)
	GetMany(ctx context.Context, in GetManyOrderInput) (GetManyOrderOutput, error)
	List(ctx context.Context, in ListOrderInput) ([]models.Order, error)

	Consumer
}

type Consumer interface {
	HandlePaymentCompleted(ctx context.Context, in HandlePaymentCompletedInput) error
	HandlePaymentFailed(ctx context.Context, in HandlePaymentFailedInput) error
}
