package errors

import (
	"encoding/json"
	"net/http"
	"runtime/debug"

	"github.com/jarviisha/darkvoid/pkg/logger"
)

// ErrorResponse represents the JSON error response structure
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error details for API responses
type ErrorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// ToResponse converts AppError to ErrorResponse
func (e *AppError) ToResponse() *ErrorResponse {
	return &ErrorResponse{
		Error: ErrorDetail{
			Code:    e.Code,
			Message: e.Message,
			Details: e.Details,
		},
	}
}

// WriteHTTP writes the error as JSON HTTP response
func (e *AppError) WriteHTTP(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.HTTPStatus)
	_ = json.NewEncoder(w).Encode(e.ToResponse())
}

// WriteJSON writes any error as a JSON HTTP response
func WriteJSON(w http.ResponseWriter, err error) {
	appErr := GetAppError(err)
	if appErr == nil {
		// Unknown error - return generic internal error
		appErr = NewInternalError(err)
	}
	appErr.WriteHTTP(w)
}

// ErrorHandler is a middleware that recovers from panics and returns error response
func ErrorHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error(r.Context(), "panic recovered", "panic", recovered, "stack", string(debug.Stack()))
				ErrInternal.WriteHTTP(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
