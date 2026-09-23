package httputil

import (
	"bytes"
	"encoding/json"
	"net/http"

	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// RESTful API Response Guidelines:
// - Success (2xx): Return data directly at top-level
// - Error (4xx/5xx): Handled by pkg/errors with standard format

// WriteJSON writes JSON response with given status code
func WriteJSON(w http.ResponseWriter, status int, data any) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(data); err != nil {
		apperrors.ErrInternal.WriteHTTP(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

// MessageResponse represents a simple message response
// Used for operations that don't return data (e.g., delete, update without body)
type MessageResponse struct {
	Message string `json:"message" example:"Operation completed successfully"`
}

// NewMessageResponse creates a simple message response
func NewMessageResponse(message string) MessageResponse {
	return MessageResponse{
		Message: message,
	}
}
