package consumer

import "errors"

// PermanentError marks a message no retry can fix -- a payload the handler
// cannot interpret.
//
// wrapped   -> DLQ now; retrying stalls the partition and every release behind it
// unwrapped -> transient (Redis down, a blip); retried before it is parked
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
