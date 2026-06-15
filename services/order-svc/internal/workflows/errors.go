package workflows

import "errors"

var (
	ErrOrderNotFound          = errors.New("order not found")
	ErrOrderAlreadyProcessed  = errors.New("order already completed or cancelled")
	ErrInventoryReserveFailed = errors.New("failed to reserve inventory")
	ErrPaymentFailed          = errors.New("payment processing failed")
	ErrPaymentTimeout         = errors.New("payment timeout exceeded")
	ErrInvalidOrderStatus     = errors.New("invalid order status for operation")
	ErrInsufficientInventory  = errors.New("insufficient inventory")
)
