package kafka

import "time"

// Events published BY Waitroom Service

type QueueReadyEvent struct {
	SessionID     string    `json:"session_id"`
	UserID        string    `json:"user_id"`
	EventID       string    `json:"event_id"`
	CheckoutToken string    `json:"checkout_token"`
	AdmittedAt    time.Time `json:"admitted_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Timestamp     time.Time `json:"timestamp"`
}

type QueueJoinedEvent struct {
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	EventID   string    `json:"event_id"`
	Position  int64     `json:"position"`
	JoinedAt  time.Time `json:"joined_at"`
	Timestamp time.Time `json:"timestamp"`
}

type QueueLeftEvent struct {
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	EventID   string    `json:"event_id"`
	Reason    string    `json:"reason"` // user_left, timeout, expired
	LeftAt    time.Time `json:"left_at"`
	Timestamp time.Time `json:"timestamp"`
}

// Events consumed BY Waitroom Service (from Checkout Service)

type CheckoutCompletedEvent struct {
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
}

type CheckoutFailedEvent struct {
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
}

type CheckoutExpiredEvent struct {
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	EventID   string    `json:"event_id"`
	ExpiredAt time.Time `json:"expired_at"`
	Timestamp time.Time `json:"timestamp"`
}

type Ticket struct {
	ID       string  `json:"id"`
	SeatNo   string  `json:"seat_no"`
	Category string  `json:"category"`
	Price    float64 `json:"price"`
}
