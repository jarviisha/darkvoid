package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jarviisha/darkvoid/pkg/config"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// errors.ErrorHandler is the only panic recovery in the global stack, and its
// position is what makes that work: registered after HTTPMiddleware it is the
// inner handler, so it sees the panic first. Nothing else asserted it was still
// registered — a reordering or a deletion would surface as a dropped connection
// under load rather than as a failing test, which is how the redundant
// chimiddleware.Recoverer went unnoticed for as long as it did.
func TestNewServer_PanicAnswersJSONErrorEnvelope(t *testing.T) {
	srv, err := NewServer(&config.Config{
		Server: config.ServerConfig{
			RateLimitRequests: 100,
			RateLimitWindow:   time.Minute,
		},
	}, logger.New(&logger.Config{Level: "error", Format: "json", Output: io.Discard}), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	srv.Router().Get("/boom", func(http.ResponseWriter, *http.Request) {
		panic("handler exploded")
	})

	recorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json — chi's Recoverer writes text/plain", got)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if body.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("error code = %q, want INTERNAL_ERROR", body.Error.Code)
	}
	if body.Error.Message == "handler exploded" {
		t.Fatal("panic value leaked into the response body")
	}
}
