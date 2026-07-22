package service

import (
	"time"
)

type CreateTicketClassInput struct {
	EventID     string
	Name        string
	PriceCents  int64
	Currency    string
	Total       int
	SaleStartAt *time.Time
	SaleEndAt   *time.Time
}

// UpdateTicketClassInput is a partial update: a nil field means "leave this
// column alone". There is deliberately no way to express reserved or sold --
// those belong exclusively to the reservation flow's locked transactions.
type UpdateTicketClassInput struct {
	Name        *string
	PriceCents  *int64
	Currency    *string
	Total       *int
	SaleStartAt *time.Time
	SaleEndAt   *time.Time
	Status      *string
}

type CheckAvailabilityInput struct {
	TicketClassID int64
	Qty           int
}

type GetManyTicketClassInput struct {
	EventID string
	IDs     []int64
}
