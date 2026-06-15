package repository

import (
	"time"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/pkg/paginator"
)

type CreateOrderOption struct {
	SessionID     string
	Code          string
	UserID        string
	UserFullName  string
	Email         string
	Phone         string
	EventID       string
	TotalAmount   int64
	Currency      string
	PaymentMethod models.PaymentMethod
	Status        models.OrderStatus
}

type UpdateOrderOption struct {
	Model  models.Order
	Status models.OrderStatus
	PaidAt *time.Time
}

type GetManyOrderOption struct {
	order.FilterOrder
	Pag paginator.PaginatorQuery
}

type ListOrderOption struct {
	order.FilterOrder
}

type GetOneOrderOption struct {
	order.FilterOrder
}
