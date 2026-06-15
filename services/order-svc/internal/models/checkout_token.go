package models

type CheckoutTokenClaim struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	EventID   string `json:"event_id"`
}
