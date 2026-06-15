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

type UpdateTicketClassInput struct {
	Name        string
	PriceCents  *int64
	Currency    string
	Total       int
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
