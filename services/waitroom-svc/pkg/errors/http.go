package errors

type HTTPError struct {
	Code       int
	Message    string
	StatusCode int
}

func NewHTTPError(code int, message string) *HTTPError {
	return &HTTPError{
		Code:    code,
		Message: message,
	}
}

func (e HTTPError) Error() string {
	return e.Message
}
