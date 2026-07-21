package consumer

import "errors"

// PermanentError marks a message that will never succeed however many times it
// is retried -- a payload the handler cannot interpret. Retrying one would stall
// the partition and every checkout release queued behind it, so these go
// straight to the dead-letter topic.
//
// Anything not wrapped this way is treated as transient (Redis unreachable, a
// downstream blip) and is retried before it is parked.
type PermanentError struct {
	Err error
}

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

func (e *PermanentError) Error() string {
	return "permanent: " + e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

func isPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}
