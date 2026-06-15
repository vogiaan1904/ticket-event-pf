package models

import (
	"time"
)

type OrderItem struct {
	// DynamoDB keys for single-table design
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`

	// Entity type for single-table design
	EntityType string `dynamodbav:"entity_type"`

	// Business fields
	ID              string     `dynamodbav:"id"`
	OrderCode       string     `dynamodbav:"order_code"`
	TicketClassID   string     `dynamodbav:"ticket_class_id"`
	TicketClassName string     `dynamodbav:"ticket_class_name"`
	PriceAtPurchase int64      `dynamodbav:"price_at_purchase"`
	Quantity        int32      `dynamodbav:"quantity"`
	TotalAmount     int64      `dynamodbav:"total_amount"`
	CreatedAt       time.Time  `dynamodbav:"created_at"`
	UpdatedAt       time.Time  `dynamodbav:"updated_at"`
	DeletedAt       *time.Time `dynamodbav:"deleted_at,omitempty"`
}
