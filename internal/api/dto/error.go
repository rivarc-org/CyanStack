package dto

// ErrorResponse is the standard error response format
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}

// Error codes
const (
	ErrCodeNotFound         = "NOT_FOUND"
	ErrCodeValidation       = "VALIDATION_ERROR"
	ErrCodeConflict         = "CONFLICT"
	ErrCodeInternal         = "INTERNAL_ERROR"
	ErrCodeDeploymentFailed = "DEPLOYMENT_FAILED"
	ErrCodeCryptoError      = "CRYPTO_ERROR"
)

// NewErrorResponse creates a new error response
func NewErrorResponse(message, code string) ErrorResponse {
	return ErrorResponse{
		Error: message,
		Code:  code,
	}
}

// NewErrorResponseWithDetails creates a new error response with additional details
func NewErrorResponseWithDetails(message, code string, details any) ErrorResponse {
	return ErrorResponse{
		Error:   message,
		Code:    code,
		Details: details,
	}
}
