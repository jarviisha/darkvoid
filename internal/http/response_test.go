package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

func TestWriteJSON_Success(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteJSON(recorder, http.StatusCreated, MessageResponse{Message: "created"})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	var response MessageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Message != "created" {
		t.Fatalf("message = %q, want created", response.Message)
	}
}

func TestWriteJSON_EncodeErrorReturnsCleanInternalError(t *testing.T) {
	recorder := httptest.NewRecorder()
	unencodable := struct {
		Values chan string `json:"values"`
	}{
		Values: make(chan string),
	}

	WriteJSON(recorder, http.StatusAccepted, unencodable)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if strings.Contains(recorder.Body.String(), "Failed to encode JSON response") {
		t.Fatalf("body contains a mixed plain-text error: %q", recorder.Body.String())
	}

	var response apperrors.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v; body = %q", err, recorder.Body.String())
	}
	if response.Error.Code != apperrors.ErrInternal.Code {
		t.Fatalf("error code = %q, want %q", response.Error.Code, apperrors.ErrInternal.Code)
	}
	if response.Error.Message != apperrors.ErrInternal.Message {
		t.Fatalf("error message = %q, want %q", response.Error.Message, apperrors.ErrInternal.Message)
	}
}
