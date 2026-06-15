package order

import (
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/pkg/paginator"
)

type CreateOrderInput struct {
	CheckoutToken string
	UserID        string
	UserFullName  string
	Email         string
	Phone         string
	EventID       string
	Currency      string
	RedirectUrl   string
	PaymentMethod models.PaymentMethod
	Items         []OrderItemInput
}

type CreateOrderOutput struct {
	Order      *models.Order
	OrderItems []models.OrderItem
	PaymentUrl string
}

type OrderItemInput struct {
	TicketClassID string
	Quantity      int32
}

type FilterOrder struct {
	Code      string
	UserID    string
	EventID   string
	SessionID string
	Status    *models.OrderStatus
}

type GetManyOrderInput struct {
	FilterOrder
	Pag paginator.PaginatorQuery
}

type GetManyOrderOutput struct {
	Orders []models.Order
	Pag    paginator.Paginator
}

type ListOrderInput struct {
	FilterOrder
}

type GetOneOrderInput struct {
	FilterOrder
}

type ReservedTicket struct {
	OrderCode     string
	TicketClassID string
	Quantity      int32
	ReservationID string
}
