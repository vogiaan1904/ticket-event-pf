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

	// The slot is held by an order in a state this service cannot resume. The
	// workflows package has its own same-named sentinel; neither survives
	// Temporal's failure proto, and the service layer must not import it.
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

// Temporal ApplicationError type strings -- the contract between a workflow and
// its callers, because a failure-proto round trip destroys sentinel identity and
// errors.Is against the vars above can never match. This holds at every boundary,
// not just the outermost: an activity's error is recorded to history the same
// way, so workflows must key off these strings too.
const (
	ErrTypeInsufficientInventory  = "InsufficientInventory"
	ErrTypeOrderCodeCollision     = "OrderCodeCollision"
	ErrTypeInventoryCannotConfirm = "InventoryCannotConfirm"
	ErrTypeOrderAlreadyProcessed  = "OrderAlreadyProcessed"
	ErrTypeOrderNotFound          = "OrderNotFound"
)
