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

	// Another order already holds this buyer's purchase slot. The caller
	// should return that order rather than creating a second one.
	ErrPurchaseSlotTaken = errors.New("purchase slot already taken")

	// The buyer's slot is held by an order that is finished. They are told to
	// start again rather than being handed a dead order. The workflows package
	// declares its own sentinel of the same name; the service layer must not
	// import it, and neither survives Temporal's failure proto anyway.
	ErrOrderAlreadyProcessed = errors.New("order already processed")

	ErrRequestTimeout = errors.New("order creation timed out")
)

// Temporal ApplicationError type strings.
//
// A workflow failure reaches the caller as a *temporal.WorkflowExecutionError
// rebuilt from a serialised failure proto, so sentinel identity does not survive
// the round trip and errors.Is against the vars above can never match. The
// ApplicationError type string does survive, which makes it the contract between
// the workflow and the gRPC layer.
const (
	ErrTypeInsufficientInventory = "InsufficientInventory"
	ErrTypeOrderCodeCollision    = "OrderCodeCollision"
)
