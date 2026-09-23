package service

import (
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
)

type JoinQueueInput struct {
	UserID    string
	EventID   string
	UserAgent string
	IPAddress string
}

type JoinQueueOutput struct {
	SessionID    string
	Position     int64
	QueueLength  int64
	QueuedAt     time.Time
	ExpiresAt    time.Time
	WebSocketURL string
}

type QueueStatusOutput struct {
	SessionID         string
	Status            models.SessionStatus
	Position          int64
	QueueLength       int64
	QueuedAt          time.Time
	ExpiresAt         time.Time
	CheckoutToken     string
	CheckoutURL       string
	CheckoutExpiresAt *time.Time
	AdmittedAt        *time.Time
}

type LeaveQueueOutput struct {
	SessionID string
	Message   string
}

type CheckoutCompletedInput struct {
	SessionID string
	UserID    string
	EventID   string
	Timestamp time.Time
}

type CheckoutFailedInput struct {
	SessionID string
	UserID    string
	EventID   string
	Timestamp time.Time
}

type CheckoutExpiredInput struct {
	SessionID string
	UserID    string
	EventID   string
	ExpiredAt time.Time
	Timestamp time.Time
}

type QueueInfoOutput struct {
	EventID         string
	QueueLength     int64
	ProcessingCount int64
}
