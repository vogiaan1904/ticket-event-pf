package handler

import "errors"

var (
	ErrSessionNotFound      = errors.New("session not found")
	ErrSessionExpired       = errors.New("session expired")
	ErrSessionAlreadyExists = errors.New("session already exists for this user and event")
	ErrQueueFull            = errors.New("queue is full")
	ErrInvalidSessionStatus = errors.New("invalid session status")
	ErrEventNotFound        = errors.New("event not found")
	ErrQueueNotEnabled      = errors.New("queue not enabled for this event")
)
