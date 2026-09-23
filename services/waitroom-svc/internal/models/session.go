package models

import (
	"math/rand/v2"
	"time"
)

type Session struct {
	ID                      string        `json:"id"`
	UserID                  string        `json:"user_id"`
	EventID                 string        `json:"event_id"`
	Position                int64         `json:"position"`
	QueuedAt                time.Time     `json:"queued_at"`
	Status                  SessionStatus `json:"status"`
	AdmittedAt              *time.Time    `json:"admitted_at,omitempty"`
	CompletedAt             *time.Time    `json:"completed_at,omitempty"`
	ExpiresAt               time.Time     `json:"expires_at"`
	CheckoutToken           string        `json:"checkout_token,omitempty"`
	CheckoutExpiresAt       *time.Time    `json:"checkout_expires_at,omitempty"`
	TokenInvalidated        bool          `json:"token_invalidated"`
	TokenInvalidatedAt      *time.Time    `json:"token_invalidated_at,omitempty"`
	TokenInvalidationReason string        `json:"token_invalidation_reason,omitempty"`
	ConnectionID            string        `json:"connection_id,omitempty"`
	UserAgent               string        `json:"user_agent,omitempty"`
	IPAddress               string        `json:"ip_address,omitempty"`
	LastHeartbeatAt         time.Time     `json:"last_heartbeat_at"`
	AttemptCount            int           `json:"attempt_count"`
	QueueScore              float64       `json:"queue_score,omitempty"`
	CreatedAt               time.Time     `json:"created_at"`
	UpdatedAt               time.Time     `json:"updated_at"`
}

type SessionStatus string

const (
	SessionStatusQueued    SessionStatus = "queued"
	SessionStatusAdmitted  SessionStatus = "admitted"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusExpired   SessionStatus = "expired"
	SessionStatusAbandoned SessionStatus = "abandoned"
	SessionStatusFailed    SessionStatus = "failed"
	SessionStatusEvicted   SessionStatus = "evicted"
)

func (s *Session) IsActive() bool {
	return s.Status == SessionStatusQueued || s.Status == SessionStatusAdmitted
}

func (s *Session) IsTerminal() bool {
	return s.Status == SessionStatusCompleted ||
		s.Status == SessionStatusExpired ||
		s.Status == SessionStatusAbandoned ||
		s.Status == SessionStatusFailed ||
		s.Status == SessionStatusEvicted
}

func (s *Session) CanAdmit() bool {
	return s.Status == SessionStatusQueued && time.Now().Before(s.ExpiresAt)
}

func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *Session) HasCheckoutExpired() bool {
	if s.CheckoutExpiresAt == nil {
		return false
	}
	return time.Now().After(*s.CheckoutExpiresAt)
}

// GetQueueScore returns the session's fixed place in the queue's ordering.
//
// The score is drawn once, at join, and carried on the session: re-deriving it
// would move someone already standing in line. A zero score is a session stored
// before the draw existed, which orders by arrival as it always did.
func (s *Session) GetQueueScore() float64 {
	if s.QueueScore != 0 {
		return s.QueueScore
	}
	return float64(s.QueuedAt.Unix())
}

// DrawQueueScore fixes where a joiner stands, once.
//
//	before the sale opens -> a random point in the second before it
//	at or after           -> arrival time
//
// Ordering everyone who gathered before the doors by arrival makes an on-sale a
// race on round-trip time, which is the one race a bot always wins.
func DrawQueueScore(queuedAt, saleStartAt time.Time) float64 {
	if saleStartAt.IsZero() || !queuedAt.Before(saleStartAt) {
		return float64(queuedAt.Unix())
	}
	// [saleStart-1, saleStart): every pre-open entrant sorts ahead of every
	// latecomer, and among themselves by lot.
	return float64(saleStartAt.Unix()-1) + rand.Float64()
}
