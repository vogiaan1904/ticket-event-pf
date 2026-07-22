package service

import "errors"

var (
	// ErrStateConflict signals a reservation is in a state that forbids the
	// requested transition (e.g. releasing a confirmed hold). Distinct from
	// insufficient-stock and not-found.
	ErrStateConflict = errors.New("reservation state conflict")

	// ErrInsufficientStock signals the requested quantity exceeds what is
	// available (total - reserved - sold) on at least one ticket class.
	ErrInsufficientStock = errors.New("insufficient stock")

	// ErrNotFound signals a referenced ticket class or order code does not
	// exist. Callers map this to gRPC NotFound.
	ErrNotFound = errors.New("resource not found")

	// ErrInventoryDrift signals the counters disagree with the reservation
	// rows -- a reserved counter lower than the holds that claim it. This is
	// corruption, not a user error: it means something wrote a quantity
	// outside a locked transaction. Always alarm-worthy.
	ErrInventoryDrift = errors.New("inventory counter drift detected")

	// ErrSaleClosed signals the ticket class is not currently on sale --
	// INACTIVE, or outside its [sale_start_at, sale_end_at] window.
	ErrSaleClosed = errors.New("ticket class not on sale")
)
