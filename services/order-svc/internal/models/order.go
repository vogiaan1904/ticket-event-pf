package models

import (
	"time"
)

type Order struct {
	// DynamoDB keys for single-table design
	PK     string `dynamodbav:"PK"`
	SK     string `dynamodbav:"SK"`
	GSI1PK string `dynamodbav:"GSI1PK,omitempty"`
	GSI1SK string `dynamodbav:"GSI1SK,omitempty"`
	GSI2PK string `dynamodbav:"GSI2PK,omitempty"`
	GSI2SK string `dynamodbav:"GSI2SK,omitempty"`

	// Entity type for single-table design
	EntityType string `dynamodbav:"entity_type"`

	// Business fields
	Code          string        `dynamodbav:"code"`
	SessionID     string        `dynamodbav:"session_id,omitempty"`
	UserID        string        `dynamodbav:"user_id"`
	UserFullName  string        `dynamodbav:"user_full_name"`
	Email         string        `dynamodbav:"email"`
	Phone         string        `dynamodbav:"phone"`
	EventID       string        `dynamodbav:"event_id"`
	TotalAmount   int64         `dynamodbav:"total_amount"`
	Currency      string        `dynamodbav:"currency"`
	PaymentMethod PaymentMethod `dynamodbav:"payment_method"`
	Status        OrderStatus   `dynamodbav:"status"`
	PaidAt        *time.Time    `dynamodbav:"paid_at,omitempty"`
	CreatedAt     time.Time     `dynamodbav:"created_at"`
	UpdatedAt     time.Time     `dynamodbav:"updated_at"`
	DeletedAt     *time.Time    `dynamodbav:"deleted_at,omitempty"`
}

// ID returns the order code as the identifier
func (o *Order) ID() string {
	return o.Code
}

type OrderStatus string

const (
	OrderStatusPending       OrderStatus = "PENDING"
	OrderStatusTimeout       OrderStatus = "TIMEOUT"
	OrderStatusCompleted     OrderStatus = "COMPLETED"
	OrderStatusCancelled     OrderStatus = "CANCELLED"
	OrderStatusPaymentFailed OrderStatus = "PAYMENT_FAILED"
	OrderStatusRefunded      OrderStatus = "REFUNDED"

	// Payment succeeded but the order cannot be fulfilled -- the stock was
	// resold before the confirmation arrived, or the order had already been
	// cancelled. The buyer is owed money. Distinct from REFUNDED, which means
	// the money has actually gone back.
	OrderStatusRefundRequired OrderStatus = "REFUND_REQUIRED"
)

type PaymentMethod string

const (
	PaymentMethodVNPAY   PaymentMethod = "VNPAY"
	PaymentMethodZalopay PaymentMethod = "ZALOPAY"
	PaymentMethodPayOS   PaymentMethod = "PAYOS"
)
