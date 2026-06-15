package temporal

// Task Queue Names
const (

	// CreateOrderTaskQueue is for API process - handles order creation
	CreateOrderTaskQueue = "create-order-tasks"

	// ConfirmOrderTaskQueue is for Consumer process - handles payment confirmations
	ConfirmOrderTaskQueue = "confirm-order-tasks"
)
