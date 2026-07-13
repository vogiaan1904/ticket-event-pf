package service

import "errors"

// ErrStateConflict signals a reservation is in a state that forbids the
// requested transition (e.g. confirming an expired hold, releasing a
// confirmed one). Distinct from insufficient-stock and not-found.
var ErrStateConflict = errors.New("reservation state conflict")
