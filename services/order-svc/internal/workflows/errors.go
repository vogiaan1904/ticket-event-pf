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

// NewInsufficientInventoryError is the sold-out rejection as it must cross the
// Temporal boundary: tagged with order.ErrTypeInsufficientInventory so the gRPC
// layer can recognise it, and non-retryable because no number of attempts makes
// stock reappear. Returning the bare sentinel instead loses both properties and
// the caller can only report a 500.
func NewInsufficientInventoryError(cause error) error {
	return temporal.NewNonRetryableApplicationError(
		ErrInsufficientInventory.Error(),
		order.ErrTypeInsufficientInventory,
		cause,
	)
}
