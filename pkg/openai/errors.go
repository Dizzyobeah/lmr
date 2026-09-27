package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ErrorType represents OpenAI API error types.
type ErrorType string

const (
	// ErrorTypeInvalidRequest indicates an invalid request.
	ErrorTypeInvalidRequest ErrorType = "invalid_request_error"

	// ErrorTypeAuthentication indicates an authentication error.
	ErrorTypeAuthentication ErrorType = "authentication_error"

	// ErrorTypePermission indicates a permission error.
	ErrorTypePermission ErrorType = "permission_error"

	// ErrorTypeNotFound indicates a resource was not found.
	ErrorTypeNotFound ErrorType = "not_found_error"

	// ErrorTypeRateLimit indicates rate limiting.
	ErrorTypeRateLimit ErrorType = "rate_limit_error"

	// ErrorTypeServer indicates a server error.
	ErrorTypeServer ErrorType = "server_error"

	// ErrorTypeServiceUnavailable indicates the service is unavailable.
	ErrorTypeServiceUnavailable ErrorType = "service_unavailable"
)

// APIError represents an OpenAI API error response.
type APIError struct {
	// Detail contains the error details. It marshals to the "error" field
	// to match the OpenAI error response format.
	Detail ErrorDetail `json:"error"`
}

// ErrorDetail contains the detailed error information.
type ErrorDetail struct {
	// Message is a human-readable error message.
	Message string `json:"message"`

	// Type is the error type.
	Type ErrorType `json:"type"`

	// Param is the parameter that caused the error (if applicable).
	Param string `json:"param,omitempty"`

	// Code is an error code (if applicable).
	Code string `json:"code,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Detail.Type, e.Detail.Message)
}

// NewAPIError creates a new APIError.
func NewAPIError(errorType ErrorType, message string, param string, code string) *APIError {
	return &APIError{
		Detail: ErrorDetail{
			Message: message,
			Type:    errorType,
			Param:   param,
			Code:    code,
		},
	}
}

// WriteError writes an API error response to the http.ResponseWriter.
// It automatically determines the status code from the error type.
func WriteError(w http.ResponseWriter, apiError *APIError) {
	statusCode := StatusCodeForError(apiError.Detail.Type)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(apiError)
}

// WriteErrorWithStatus writes an API error with a specific status code.
func WriteErrorWithStatus(w http.ResponseWriter, statusCode int, apiError *APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(apiError)
}

// Common error constructors

// ErrInvalidRequest creates an invalid request error.
func ErrInvalidRequest(message string) *APIError {
	return NewAPIError(ErrorTypeInvalidRequest, message, "", "")
}

// ErrInvalidRequestParam creates an invalid request error for a specific parameter.
func ErrInvalidRequestParam(message, param string) *APIError {
	return NewAPIError(ErrorTypeInvalidRequest, message, param, "")
}

// ErrAuthentication creates an authentication error.
func ErrAuthentication(message string) *APIError {
	return NewAPIError(ErrorTypeAuthentication, message, "", "invalid_api_key")
}

// ErrMissingAPIKey creates a missing API key error.
func ErrMissingAPIKey() *APIError {
	return NewAPIError(
		ErrorTypeAuthentication,
		"You didn't provide an API key. You need to provide your API key in an Authorization header using Bearer auth.",
		"",
		"missing_api_key",
	)
}

// ErrInvalidAPIKey creates an invalid API key error.
func ErrInvalidAPIKey() *APIError {
	return NewAPIError(
		ErrorTypeAuthentication,
		"Incorrect API key provided. You can find your API key in the admin dashboard.",
		"",
		"invalid_api_key",
	)
}

// ErrExpiredAPIKey creates an expired API key error.
func ErrExpiredAPIKey() *APIError {
	return NewAPIError(
		ErrorTypeAuthentication,
		"The API key has expired.",
		"",
		"expired_api_key",
	)
}

// ErrDisabledAPIKey creates a disabled API key error.
func ErrDisabledAPIKey() *APIError {
	return NewAPIError(
		ErrorTypeAuthentication,
		"The API key has been disabled.",
		"",
		"disabled_api_key",
	)
}

// ErrPermission creates a permission error.
func ErrPermission(message string) *APIError {
	return NewAPIError(ErrorTypePermission, message, "", "")
}

// ErrModelNotAllowed creates a model not allowed error.
func ErrModelNotAllowed(model string) *APIError {
	return NewAPIError(
		ErrorTypePermission,
		fmt.Sprintf("Your API key does not have access to the model '%s'.", model),
		"model",
		"model_not_allowed",
	)
}

// ErrNotFound creates a not found error.
func ErrNotFound(message string) *APIError {
	return NewAPIError(ErrorTypeNotFound, message, "", "")
}

// ErrModelNotFound creates a model not found error.
func ErrModelNotFound(model string) *APIError {
	return NewAPIError(
		ErrorTypeNotFound,
		fmt.Sprintf("The model '%s' does not exist or you do not have access to it.", model),
		"model",
		"model_not_found",
	)
}

// ErrRateLimit creates a rate limit error.
func ErrRateLimit(message string) *APIError {
	return NewAPIError(ErrorTypeRateLimit, message, "", "rate_limit_exceeded")
}

// ErrRateLimitExceeded creates a rate limit exceeded error with retry info.
func ErrRateLimitExceeded(requestsPerMinute int) *APIError {
	return NewAPIError(
		ErrorTypeRateLimit,
		fmt.Sprintf("Rate limit exceeded. Maximum %d requests per minute.", requestsPerMinute),
		"",
		"rate_limit_exceeded",
	)
}

// ErrServer creates a server error.
func ErrServer(message string) *APIError {
	return NewAPIError(ErrorTypeServer, message, "", "")
}

// ErrInternalServer creates an internal server error.
func ErrInternalServer() *APIError {
	return NewAPIError(
		ErrorTypeServer,
		"An internal server error occurred. Please try again later.",
		"",
		"internal_error",
	)
}

// ErrServiceUnavailable creates a service unavailable error.
func ErrServiceUnavailable(message string) *APIError {
	return NewAPIError(ErrorTypeServiceUnavailable, message, "", "")
}

// ErrNoModelsAvailable creates a no models available error.
func ErrNoModelsAvailable() *APIError {
	return NewAPIError(
		ErrorTypeServiceUnavailable,
		"No models are currently available. Please ensure Ollama is running and has models loaded.",
		"",
		"no_models_available",
	)
}

// ErrOllamaUnavailable creates an Ollama unavailable error.
func ErrOllamaUnavailable() *APIError {
	return NewAPIError(
		ErrorTypeServiceUnavailable,
		"Unable to connect to Ollama. Please ensure Ollama is running.",
		"",
		"ollama_unavailable",
	)
}

// ErrContextLengthExceeded creates a context length exceeded error.
func ErrContextLengthExceeded(maxTokens int) *APIError {
	return NewAPIError(
		ErrorTypeInvalidRequest,
		fmt.Sprintf("This model's maximum context length is %d tokens. Please reduce the length of your messages.", maxTokens),
		"messages",
		"context_length_exceeded",
	)
}

// ErrInvalidModel creates an invalid model error.
func ErrInvalidModel(model string) *APIError {
	return NewAPIError(
		ErrorTypeInvalidRequest,
		fmt.Sprintf("Invalid model: %s", model),
		"model",
		"invalid_model",
	)
}

// ErrInvalidMessages creates an invalid messages error.
func ErrInvalidMessages(message string) *APIError {
	return NewAPIError(ErrorTypeInvalidRequest, message, "messages", "invalid_messages")
}

// ErrEmptyMessages creates an empty messages error.
func ErrEmptyMessages() *APIError {
	return NewAPIError(
		ErrorTypeInvalidRequest,
		"Messages cannot be empty.",
		"messages",
		"empty_messages",
	)
}

// StatusCodeForError returns the appropriate HTTP status code for an error type.
func StatusCodeForError(errorType ErrorType) int {
	switch errorType {
	case ErrorTypeInvalidRequest:
		return http.StatusBadRequest
	case ErrorTypeAuthentication:
		return http.StatusUnauthorized
	case ErrorTypePermission:
		return http.StatusForbidden
	case ErrorTypeNotFound:
		return http.StatusNotFound
	case ErrorTypeRateLimit:
		return http.StatusTooManyRequests
	case ErrorTypeServer:
		return http.StatusInternalServerError
	case ErrorTypeServiceUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// ErrUnauthorized creates an unauthorized error.
func ErrUnauthorized(message string) *APIError {
	return NewAPIError(ErrorTypeAuthentication, message, "", "unauthorized")
}

// ErrForbidden creates a forbidden error.
func ErrForbidden(message string) *APIError {
	return NewAPIError(ErrorTypePermission, message, "", "forbidden")
}

// ErrInternal creates an internal error.
func ErrInternal(message string) *APIError {
	return NewAPIError(ErrorTypeServer, message, "", "internal_error")
}
