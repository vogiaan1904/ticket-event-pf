package service

import (
	"time"
)

type ReserveInput struct {
	OrderCode string
	Items     []ReserveItem
	ExpiresAt time.Time
}

type ReserveItem struct {
	TicketClassID int64
	Qty           int
}
