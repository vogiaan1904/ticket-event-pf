package repository

import (
	"context"
	"time"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/pkg/paginator"
)

type Repository interface {
	OrderRepository
	OrderItemRepository
}

type OrderRepository interface {
	Create(ctx context.Context, opt CreateOrderOption) (models.Order, error)
	ClaimPurchaseSlot(ctx context.Context, dedupeKey, orderCode string) (string, time.Time, error)
	ReleasePurchaseSlot(ctx context.Context, dedupeKey, orderCode string) error
	GetByCode(ctx context.Context, code string) (models.Order, error)
	GetOne(ctx context.Context, opt GetOneOrderOption) (models.Order, error)
	GetMany(ctx context.Context, opt GetManyOrderOption) ([]models.Order, paginator.Paginator, error)
	List(ctx context.Context, opt ListOrderOption) ([]models.Order, error)
	Update(ctx context.Context, code string, opt UpdateOrderOption) (models.Order, error)
	Delete(ctx context.Context, code string) error
}

type OrderItemRepository interface {
	CreateManyItems(ctx context.Context, orderCode string, opts []CreateOrderItemOption) ([]models.OrderItem, error)
	ListItemByOrderCode(ctx context.Context, orderCode string) ([]models.OrderItem, error)
	DeleteItemByOrderCode(ctx context.Context, orderCode string) error
}
