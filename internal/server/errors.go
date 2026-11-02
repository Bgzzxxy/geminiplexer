package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// APIError represents a structured error response following OpenAI conventions.
type APIError struct {
	Error APIErrorBody `json:"error"`
}

type APIErrorBody struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Code    string  `json:"code"`
	Param   *string `json:"param,omitempty"`
}

// ErrorCode constants
const (
	ErrCodeAuthenticationError = "authentication_error"
	ErrCodeUpstreamAuthError   = "upstream_auth_error"
	ErrCodeAccountBanned       = "account_banned"
	ErrCodeModelNotFound       = "model_not_found"
	ErrCodeRateLimitError      = "rate_limit_error"
	ErrCodeModelSubstitution   = "model_substitution"
	ErrCodeAuthUnavailable     = "auth_unavailable"
	ErrCodeSessionBusy         = "session_busy"
	ErrCodeInvalidRequest      = "invalid_request_error"
	ErrCodeServerError         = "server_error"
)

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIError{
		Error: APIErrorBody{
			Message: message,
			Type:    "error",
			Code:    code,
		},
	})
}

func writeRateLimitError(w http.ResponseWriter, code, message string, resetAt time.Time) {
	w.Header().Set("Content-Type", "application/json")
	if !resetAt.IsZero() {
		w.Header().Set("X-Resets-At", resetAt.Format(time.RFC3339))
		remaining := time.Until(resetAt).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		w.Header().Set("Retry-After", json.Number(json.Number(time.Duration(remaining*float64(time.Second)).String())).String())
	}
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(APIError{
		Error: APIErrorBody{
			Message: message,
			Type:    "error",
			Code:    code,
		},
	})
}

func writeModelSubstitutionError(w http.ResponseWriter, requested, actual string, resetAt time.Time) {
	msg := "Requested model '" + requested + "' was silently downgraded to '" + actual + "'. Retry after quota reset or use a different account."
	writeRateLimitError(w, ErrCodeModelSubstitution, msg, resetAt)
}
