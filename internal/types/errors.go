package types

import (
	"encoding/json"
	"net/http"
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string {
	return e.Message
}

type ErrorResponse struct {
	Error *APIError `json:"error"`
}

var (
	ErrInvalidCredentials = &APIError{Code: "invalid_credentials", Message: "Invalid email or password", Status: http.StatusUnauthorized}
	ErrEmailTaken         = &APIError{Code: "email_taken", Message: "Email is already taken", Status: http.StatusConflict}
	ErrUnauthorized       = &APIError{Code: "unauthorized", Message: "Unauthorized", Status: http.StatusUnauthorized}
	ErrForbidden          = &APIError{Code: "forbidden", Message: "Forbidden", Status: http.StatusForbidden}
	ErrNotFound           = &APIError{Code: "not_found", Message: "Not found", Status: http.StatusNotFound}
	ErrTaskRunning        = &APIError{Code: "task_running", Message: "A task is already running for this project", Status: http.StatusConflict}
	ErrInsufficientCredits = &APIError{Code: "insufficient_credits", Message: "Insufficient credits", Status: http.StatusPaymentRequired}
	ErrSlugTaken          = &APIError{Code: "slug_taken", Message: "Slug is already taken", Status: http.StatusConflict}
	ErrSlugReserved       = &APIError{Code: "slug_reserved", Message: "Slug is reserved", Status: http.StatusBadRequest}
	ErrNodeUnavailable    = &APIError{Code: "node_unavailable", Message: "Node is unavailable", Status: http.StatusServiceUnavailable}
	ErrRateLimited        = &APIError{Code: "rate_limited", Message: "Rate limited", Status: http.StatusTooManyRequests}
	ErrValidationFailed   = &APIError{Code: "validation_failed", Message: "Validation failed", Status: http.StatusBadRequest}
	ErrProjectCold        = &APIError{Code: "project_cold", Message: "Project is cold", Status: http.StatusConflict}
)

func WriteError(w http.ResponseWriter, apiErr *APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiErr.Status)
	json.NewEncoder(w).Encode(ErrorResponse{Error: apiErr})
}