package kafka

import (
	"encoding/json"
	"strings"
	"time"
)

// EventTime is a wire timestamp that never fails to decode.
// Anything unparseable becomes the zero time and the handler substitutes the
// broker timestamp.
// Why: the field is bookkeeping, and strict decoding would stall slot releases.
type EventTime struct {
	time.Time
}

func (t *EventTime) UnmarshalJSON(b []byte) error {
	t.Time = time.Time{}

	var s string
	if err := json.Unmarshal(b, &s); err != nil || strings.TrimSpace(s) == "" {
		return nil
	}

	if parsed, err := time.Parse(time.RFC3339, s); err == nil {
		t.Time = parsed
	}

	return nil
}

func (t EventTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return json.Marshal("")
	}
	return json.Marshal(t.UTC().Format(time.RFC3339))
}

// OrElse returns the decoded timestamp, or fallback when it was absent or
// malformed on the wire.
func (t EventTime) OrElse(fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback
	}
	return t.Time
}

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
	Timestamp EventTime `json:"timestamp"`
}

type CheckoutFailedEvent struct {
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	EventID   string    `json:"event_id"`
	Timestamp EventTime `json:"timestamp"`
}

type CheckoutExpiredEvent struct {
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	EventID   string    `json:"event_id"`
	ExpiredAt EventTime `json:"expired_at"`
	Timestamp EventTime `json:"timestamp"`
}

type Ticket struct {
	ID       string  `json:"id"`
	SeatNo   string  `json:"seat_no"`
	Category string  `json:"category"`
	Price    float64 `json:"price"`
}
