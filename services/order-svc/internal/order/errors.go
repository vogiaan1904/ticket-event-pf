package order

import "errors"

var (
	ErrOrderNotFound           = errors.New("order not found")
	ErrOrderAlreadyExists      = errors.New("order already exists")
	ErrInvalidOrderStatus      = errors.New("invalid order status")
	ErrOrderCreationFailed     = errors.New("order creation failed")
	ErrOrderUpdateFailed       = errors.New("order update failed")
	ErrOrderCancellationFailed = errors.New("order cancellation failed")
	ErrOrderNotPending         = errors.New("order is not in pending status")
	ErrPaymentAmountMismatch   = errors.New("payment amount does not match order amount")

	ErrEventNotFound        = errors.New("event not found")
	ErrEventNotReadyForSale = errors.New("event not ready for sale")
	ErrTicketClassNotFound  = errors.New("ticket class not found")
	ErrTicketSoldOut        = errors.New("ticket sold out")
	ErrNotEnoughTickets     = errors.New("not enough tickets available")
	ErrEventConfigNotFound  = errors.New("event config not found")

	ErrInvalidCheckoutToken = errors.New("invalid checkout token")
)
