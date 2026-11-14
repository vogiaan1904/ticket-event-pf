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

// ReleaseReason decides the terminal status a release writes.
//
//	ReleaseUndone        -> CANCELLED: final; Confirm refuses it
//	ReleaseCheckoutEnded -> EXPIRED: a late payment may re-acquire the seat
type ReleaseReason int

const (
	ReleaseUndone ReleaseReason = iota
	ReleaseCheckoutEnded
)
