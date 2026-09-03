# Ticket Inventory System - Models Summary

## Overview

This document explains the GORM models for the ticket inventory system, including table structures, relationships, and usage examples.

## Database Tables

### 1. `ticket_class` Table

Represents different types of tickets for an event (e.g., General Admission, VIP, Seat Zone A).

**Fields:**

- `id` (BIGSERIAL) - Primary key
- `event_id` (BIGINT) - Foreign key to events (not null)
- `name` (TEXT) - Ticket type name (not null)
- `price_cents` (INT) - Price in cents (not null)
- `currency` (TEXT) - Currency code (not null)
- `total` (INT) - Total available tickets (not null)
- `reserved` (INT) - Currently reserved tickets (default: 0)
- `sold` (INT) - Sold tickets (default: 0)
- `sale_start_at` (TIMESTAMPTZ) - Sale start time (nullable)
- `sale_end_at` (TIMESTAMPTZ) - Sale end time (nullable)
- `status` (TEXT) - Status (default: 'ACTIVE')
- `created_at` (TIMESTAMPTZ) - Auto-generated
- `updated_at` (TIMESTAMPTZ) - Auto-updated

**Constraints:**

- Unique constraint on `(event_id, name)`

**Available Count Formula:**

```
available = total - reserved - sold
```

### 2. `reservation` Table

Represents short-lived ticket holds created by the Order service.

**Fields:**

- `id` (BIGSERIAL) - Primary key
- `order_id` (UUID) - Order identifier (not null)
- `ticket_class_id` (BIGINT) - Foreign key to ticket_class (not null)
- `qty` (INT) - Quantity reserved (not null)
- `expires_at` (TIMESTAMPTZ) - Expiration time (not null)
- `status` (TEXT) - Status: ACTIVE, CONFIRMED, EXPIRED, CANCELLED (not null)
- `created_at` (TIMESTAMPTZ) - Auto-generated
- `updated_at` (TIMESTAMPTZ) - Auto-updated

**Constraints:**

- Unique constraint on `(order_id, ticket_class_id)`
- Check constraint: `status IN ('ACTIVE','CONFIRMED','EXPIRED','CANCELLED')`
- Index on `(ticket_class_id, status, expires_at)` for efficient expiration cleanup

## GORM Models

### TicketClass Model

```go
type TicketClass struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	EventID     uint      `gorm:"not null;index:idx_event_name,unique" json:"event_id"`
	Name        string    `gorm:"type:text;not null;index:idx_event_name,unique" json:"name"`
	PriceCents  int       `gorm:"type:int;not null" json:"price_cents"`
	Currency    string    `gorm:"type:text;not null" json:"currency"`
	Total       int       `gorm:"type:int;not null" json:"total"`
	Reserved    int       `gorm:"type:int;not null;default:0" json:"reserved"`
	Sold        int       `gorm:"type:int;not null;default:0" json:"sold"`
	SaleStartAt *time.Time `gorm:"type:timestamptz" json:"sale_start_at,omitempty"`
	SaleEndAt   *time.Time `gorm:"type:timestamptz" json:"sale_end_at,omitempty"`
	Status      string    `gorm:"type:text;not null;default:'ACTIVE'" json:"status"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	// Relations
	Reservations []Reservation `gorm:"foreignKey:TicketClassID;constraint:OnDelete:CASCADE" json:"reservations,omitempty"`
}
```

### Reservation Model

```go
type ReservationStatus string

const (
	ReservationStatusActive    ReservationStatus = "ACTIVE"
	ReservationStatusConfirmed ReservationStatus = "CONFIRMED"
	ReservationStatusExpired   ReservationStatus = "EXPIRED"
	ReservationStatusCancelled ReservationStatus = "CANCELLED"
)

type Reservation struct {
	ID            uint              `gorm:"primaryKey;autoIncrement" json:"id"`
	OrderID       uuid.UUID         `gorm:"type:uuid;not null;index:idx_order_ticket,unique" json:"order_id"`
	TicketClassID uint              `gorm:"not null;index:idx_ticket_status_expires;index:idx_order_ticket,unique" json:"ticket_class_id"`
	Qty           int               `gorm:"type:int;not null" json:"qty"`
	ExpiresAt     time.Time         `gorm:"type:timestamptz;not null;index:idx_ticket_status_expires" json:"expires_at"`
	Status        ReservationStatus `gorm:"type:text;not null;check:status IN ('ACTIVE','CONFIRMED','EXPIRED','CANCELLED');index:idx_ticket_status_expires" json:"status"`
	CreatedAt     time.Time         `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time         `gorm:"autoUpdateTime" json:"updated_at"`

	// Relations
	TicketClass TicketClass `gorm:"foreignKey:TicketClassID;constraint:OnDelete:RESTRICT" json:"ticket_class,omitempty"`
}

// Helper methods
func (r *Reservation) IsActive() bool {
	return r.Status == ReservationStatusActive && time.Now().Before(r.ExpiresAt)
}

func (r *Reservation) IsExpired() bool {
	return time.Now().After(r.ExpiresAt) && r.Status == ReservationStatusActive
}
```

## Repository Pattern

### TicketClassRepository

Key methods for managing ticket inventory:

```go
// Querying
GetByID(ctx, id) (*TicketClass, error)
GetByEventID(ctx, eventID) ([]TicketClass, error)
GetAvailableByEventID(ctx, eventID) ([]TicketClass, error)
CheckAvailability(ctx, id, quantity) (bool, error)

// Atomic Operations
IncrementReserved(ctx, id, quantity) error
DecrementReserved(ctx, id, quantity) error
IncrementSold(ctx, id, quantity) error

// CRUD
Create(ctx, ticketClass) error
Update(ctx, ticketClass) error
Delete(ctx, id) error
```

### ReservationRepository

Key methods for managing reservations:

```go
// Querying
GetByID(ctx, id) (*Reservation, error)
GetByOrderID(ctx, orderID) ([]Reservation, error)
GetExpired(ctx, limit) ([]Reservation, error)

// Status Management
ConfirmReservation(ctx, id) error
ConfirmReservationsByOrderID(ctx, orderID) error
CancelReservation(ctx, id) error
ExpireReservation(ctx, id) error

// CRUD
Create(ctx, reservation) error
Update(ctx, reservation) error
Delete(ctx, id) error
```

## Usage Examples

### 1. Create Ticket Class

```go
ticketClass := &models.TicketClass{
	EventID:     123,
	Name:        "VIP",
	PriceCents:  15000, // $150.00
	Currency:    "USD",
	Total:       100,
	Status:      "ACTIVE",
}
err := ticketClassRepo.Create(ctx, ticketClass)
```

### 2. Reserve Tickets (with Transaction)

```go
err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
	txTicketRepo := repository.NewTicketClassRepository(tx)
	txReservationRepo := repository.NewReservationRepository(tx)

	// Check availability
	available, err := txTicketRepo.CheckAvailability(ctx, ticketClassID, qty)
	if err != nil {
		return err
	}
	if !available {
		return errors.New("not enough tickets")
	}

	// Reserve tickets
	if err := txTicketRepo.IncrementReserved(ctx, ticketClassID, qty); err != nil {
		return err
	}

	// Create reservation
	reservation := &models.Reservation{
		OrderID:       orderID,
		TicketClassID: ticketClassID,
		Qty:           qty,
		ExpiresAt:     time.Now().Add(15 * time.Minute),
		Status:        models.ReservationStatusActive,
	}
	return txReservationRepo.Create(ctx, reservation)
})
```

### 3. Confirm Purchase (Convert Reservation to Sale)

```go
err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
	txTicketRepo := repository.NewTicketClassRepository(tx)
	txReservationRepo := repository.NewReservationRepository(tx)

	// Get reservations for the order
	reservations, err := txReservationRepo.GetByOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	// Convert each reservation to a sale
	for _, res := range reservations {
		// This increments sold and decrements reserved
		if err := txTicketRepo.IncrementSold(ctx, res.TicketClassID, res.Qty); err != nil {
			return err
		}
	}

	// Mark reservations as confirmed
	return txReservationRepo.ConfirmReservationsByOrderID(ctx, orderID)
})
```

### 4. Handle Expired Reservations

```go
// Find expired reservations
expiredReservations, err := reservationRepo.GetExpired(ctx, 100)
if err != nil {
	return err
}

// Process each expired reservation
for _, expired := range expiredReservations {
	// Mark as expired
	if err := reservationRepo.ExpireReservation(ctx, expired.ID); err != nil {
		return err
	}

	// Release the reserved tickets
	if err := ticketClassRepo.DecrementReserved(ctx, expired.TicketClassID, expired.Qty); err != nil {
		return err
	}
}
```

### 5. Cancel Order

```go
err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
	txTicketRepo := repository.NewTicketClassRepository(tx)
	txReservationRepo := repository.NewReservationRepository(tx)

	// Get all reservations
	reservations, err := txReservationRepo.GetByOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	// Release reserved tickets
	for _, res := range reservations {
		if err := txTicketRepo.DecrementReserved(ctx, res.TicketClassID, res.Qty); err != nil {
			return err
		}
	}

	// Mark reservations as cancelled
	return txReservationRepo.CancelReservationsByOrderID(ctx, orderID)
})
```

## Best Practices

1. **Always use transactions** when modifying both ticket_class and reservation tables
2. **Check availability** before creating reservations
3. **Set appropriate expiration times** for reservations (typically 10-15 minutes)
4. **Run background jobs** to clean up expired reservations
5. **Use atomic operations** (IncrementReserved, IncrementSold) to avoid race conditions
6. **Index properly** for efficient queries on (ticket_class_id, status, expires_at)

## Database Migrations

To create these tables in your database:

```go
db.AutoMigrate(&models.TicketClass{}, &models.Reservation{})
```

For production, consider creating explicit migrations with proper indexes and constraints.

## Complete Example

See `/examples/ticket_inventory_example.go` for a complete working example that demonstrates:

- Creating ticket classes
- Reserving tickets with transactions
- Confirming purchases
- Handling expired reservations
- Canceling orders
