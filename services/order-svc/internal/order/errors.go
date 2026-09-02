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

	// The buyer's slot is held by an order in a state this service does not
	// know how to resume. The workflows package declares its own sentinel of
	// the same name; the service layer must not import it, and neither
	// survives Temporal's failure proto anyway.
	ErrOrderAlreadyProcessed = errors.New("order already processed")

	// The buyer's purchase slot could not be settled: it named an order that
	// was never written, or it kept changing hands. Nothing is broken and no
	// order was created, so the buyer retries into a clean slot.
	ErrPurchaseSlotUnsettled = errors.New("purchase slot could not be settled")

	ErrRequestTimeout = errors.New("order creation timed out")

	// ErrInventoryCannotConfirm is a business rejection from inventory-svc's
	// Confirm: the hold already expired and the stock was resold, or the
	// reservation is in a conflicting state. The order cannot be fulfilled by
	// retrying, unlike an infrastructure failure on the same call.
	ErrInventoryCannotConfirm = errors.New("inventory cannot confirm this order")
)

// Temporal ApplicationError type strings.
//
// A workflow failure reaches the caller as a *temporal.WorkflowExecutionError
// rebuilt from a serialised failure proto, so sentinel identity does not survive
// the round trip and errors.Is against the vars above can never match. The
// ApplicationError type string does survive, which makes it the contract between
// the workflow and the gRPC layer.
//
// It also does not survive only at that final boundary: an activity's failure
// is itself recorded to workflow history as the same kind of failure proto, so
// a workflow inspecting an activity's error is subject to the identical loss
// and must key off these same strings, not the sentinel above.
const (
	ErrTypeInsufficientInventory  = "InsufficientInventory"
	ErrTypeOrderCodeCollision     = "OrderCodeCollision"
	ErrTypeInventoryCannotConfirm = "InventoryCannotConfirm"
)
