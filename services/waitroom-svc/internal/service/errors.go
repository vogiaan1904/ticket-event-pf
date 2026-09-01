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

	// A failed call to event-svc that is not a verdict about the event. It must
	// not be folded into ErrEventNotFound: a 404 tells the buyer to stop, when
	// the truth is that the dependency is down or slow and the answer is retry.
	ErrEventServiceUnavailable = errors.New("event service unavailable")
	ErrEventServiceTimeout     = errors.New("event service timed out")

	ErrProcessorStopped = errors.New("queue processor has been stopped")
	ErrEventNotActive   = errors.New("event is not active or not found")

	// ErrSessionNotAdmittable marks an admission failure that will never
	// succeed: the session is gone, already admitted, or otherwise no longer a
	// queue member. It is the processor's signal that dropping the entry from
	// the queue is correct -- every other failure is transient and must leave
	// the user queued.
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
