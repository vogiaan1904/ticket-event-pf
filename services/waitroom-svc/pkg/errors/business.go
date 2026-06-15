package errors

type BusinessError struct {
	Code    string
	Message string
}

func NewBusinessError(code string, message string) *BusinessError {
	return &BusinessError{
		Code:    code,
		Message: message,
	}
}
