package workflows

import (
	"errors"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"go.temporal.io/sdk/temporal"
)

var (
	ErrOrderNotFound          = errors.New("order not found")
	ErrOrderAlreadyProcessed  = errors.New("order already completed or cancelled")
	ErrInventoryReserveFailed = errors.New("failed to reserve inventory")
	ErrPaymentFailed          = errors.New("payment processing failed")
	ErrPaymentTimeout         = errors.New("payment timeout exceeded")
	ErrInvalidOrderStatus     = errors.New("invalid order status for operation")
	ErrInsufficientInventory  = errors.New("insufficient inventory")
)

// NewInsufficientInventoryError carries the sold-out rejection across the
// Temporal boundary: tagged with order.ErrTypeInsufficientInventory so the gRPC
// layer can recognise it, and non-retryable because no number of attempts makes
// stock reappear.
func NewInsufficientInventoryError(cause error) error {
	return temporal.NewNonRetryableApplicationError(
		ErrInsufficientInventory.Error(),
		order.ErrTypeInsufficientInventory,
		cause,
	)
}

// NewOrderAlreadyProcessedError says the payment event landed on an order whose
// state already accounts for it. Non-retryable -- a recorded outcome does not
// change on another attempt -- and tagged so the consumer stops redelivering.
func NewOrderAlreadyProcessedError() error {
	return temporal.NewNonRetryableApplicationError(
		ErrOrderAlreadyProcessed.Error(),
		order.ErrTypeOrderAlreadyProcessed,
		ErrOrderAlreadyProcessed,
	)
}
