package service

import "errors"

var (
	ErrSessionNotFound      = errors.New("session not found")
	ErrSessionExpired       = errors.New("session expired")
	ErrSessionAlreadyExists = errors.New("session already exists for this user and event")
	ErrInvalidSessionStatus = errors.New("invalid session status")

	ErrQueueFull           = errors.New("queue is full")
	ErrEventNotFound       = errors.New("event not found")
	ErrEventConfigNotFound = errors.New("event config not found")
	ErrWaitRoomNotAllowed  = errors.New("wait room is not allowed for this event")

	// A failed call to event-svc, not a verdict about the event. Never fold into
	// ErrEventNotFound: a 404 tells the buyer to stop; the answer here is retry.
	ErrEventServiceUnavailable = errors.New("event service unavailable")
	ErrEventServiceTimeout     = errors.New("event service timed out")

	ErrProcessorStopped = errors.New("queue processor has been stopped")
	ErrEventNotActive   = errors.New("event is not active or not found")

	// ErrSessionNotAdmittable marks an admission failure that can never succeed:
	// gone, already admitted, or no longer a queue member. It is the processor's
	// signal to drop the entry; every other failure is transient and stays queued.
	ErrSessionNotAdmittable = errors.New("session is no longer admittable")

	ErrTokenEmpty               = errors.New("token cannot be empty")
	ErrTokenInvalid             = errors.New("invalid token")
	ErrTokenInvalidated         = errors.New("token has been invalidated")
	ErrTokenExpired             = errors.New("checkout token has expired")
	ErrTokenInvalidClaims       = errors.New("invalid token claims")
	ErrTokenUnexpectedSignature = errors.New("unexpected token signing method")
	ErrTokenNotValid            = errors.New("token is not valid")
	ErrSessionNotAdmitted       = errors.New("session status must be admitted")
)
