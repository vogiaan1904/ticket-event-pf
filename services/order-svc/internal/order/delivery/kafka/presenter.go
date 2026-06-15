package kafka

type PaymentCompletedEvent struct {
	OrderCode     string `json:"order_code"`
	PaidAt        string `json:"paid_at,omitempty"`
	PaymentID     string `json:"payment_id"`
	AmountCents   int64  `json:"amount_cents"`
	Currency      string `json:"currency"`
	Provider      string `json:"provider"`
	TransactionID string `json:"transaction_id"`
	CompletedAt   string `json:"completed_at"`
}

type PaymentFailedEvent struct {
	OrderCode     string `json:"order_code"`
	PaymentID     string `json:"payment_id"`
	AmountCents   int64  `json:"amount_cents"`
	Currency      string `json:"currency"`
	Provider      string `json:"provider"`
	TransactionID string `json:"transaction_id"`
	FailedAt      string `json:"failed_at"`
}
type CheckoutCompletedEvent struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	EventID   string `json:"event_id"`
	Timestamp string `json:"timestamp"`
}

type CheckoutFailedEvent struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	EventID   string `json:"event_id"`
	Timestamp string `json:"timestamp"`
}
