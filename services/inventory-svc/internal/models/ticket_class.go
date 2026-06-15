package models

import (
	"time"
)

// TicketClass represents ticket types for an event (GA, VIP, Seat Zone A, etc.)
type TicketClass struct {
	ID          int64  `gorm:"primarykey;autoIncrement"`
	EventID     string `gorm:"not null;uniqueIndex:idx_event_name;index:idx_event_id"`
	Name        string `gorm:"not null;uniqueIndex:idx_event_name"`
	PriceCents  int64  `gorm:"not null"`
	Currency    string `gorm:"not null"`
	Total       int    `gorm:"not null"`
	Reserved    int    `gorm:"not null;default:0"`
	Sold        int    `gorm:"not null;default:0"`
	SaleStartAt *time.Time
	SaleEndAt   *time.Time
	Status      TicketClassStatus `gorm:"not null;default:'ACTIVE'"`
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// Relations
	Reservations []Reservation `gorm:"constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for TicketClass
func (TicketClass) TableName() string {
	return "ticket_class"
}

type TicketClassStatus string

const (
	TicketClassStatusActive   TicketClassStatus = "ACTIVE"
	TicketClassStatusInactive TicketClassStatus = "INACTIVE"
)
