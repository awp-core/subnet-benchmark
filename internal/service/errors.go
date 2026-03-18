package service

// UserError represents an expected error caused by user input.
type UserError struct {
	Code    string
	Message string
}

func (e *UserError) Error() string {
	return e.Message
}
