package errors

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestWithDetail_DoesNotMutateReceiver(t *testing.T) {
	base := New("TEST", "test", http.StatusBadRequest)
	first := base.WithDetail("request", "first")
	second := base.WithDetail("request", "second")

	if len(base.Details) != 0 {
		t.Fatalf("base details = %v, want empty", base.Details)
	}
	if got := first.Details["request"]; got != "first" {
		t.Fatalf("first detail = %v, want first", got)
	}
	if got := second.Details["request"]; got != "second" {
		t.Fatalf("second detail = %v, want second", got)
	}
}

func TestAppError_WrapAndResponseLifecycle(t *testing.T) {
	t.Parallel()
	sentinel := stderrors.New("database unavailable")
	wrapped := Wrap(sentinel, "DB_ERROR", "database failed", http.StatusServiceUnavailable).WithDetail("operation", "read")
	if got := wrapped.Error(); !strings.Contains(got, sentinel.Error()) {
		t.Fatalf("Error() = %q, want underlying error", got)
	}
	if !stderrors.Is(wrapped, sentinel) || Unwrap(wrapped) != sentinel || GetAppError(wrapped) != wrapped {
		t.Fatal("wrapped error chain was not preserved")
	}
	var target *AppError
	if !As(wrapped, &target) || target != wrapped || !Is(wrapped, sentinel) {
		t.Fatal("As/Is did not traverse AppError")
	}
	if Wrap(nil, "IGNORED", "ignored", 500) != nil {
		t.Fatal("Wrap(nil) must return nil")
	}
	if Wrap(wrapped, "OTHER", "other", 500) != wrapped {
		t.Fatal("Wrap(AppError) must preserve the original error")
	}

	recorder := httptest.NewRecorder()
	WriteJSON(recorder, wrapped)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response status/content type = %d/%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	var response ErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Code != "DB_ERROR" || response.Error.Details["operation"] != "read" {
		t.Fatalf("response = %#v", response)
	}
}

func TestWriteErrorResponse_HidesUnknownError(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	WriteErrorResponse(recorder, stderrors.New("credential-secret"))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "credential-secret") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCommonErrorConstructors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    *AppError
		code   string
		status int
	}{
		{name: "validation", err: NewValidationError("email", "invalid"), code: "VALIDATION_ERROR", status: 400},
		{name: "not found", err: NewNotFoundError("post"), code: "NOT_FOUND", status: 404},
		{name: "conflict", err: NewConflictError("username"), code: "CONFLICT", status: 409},
		{name: "unauthorized", err: NewUnauthorizedError("expired"), code: "UNAUTHORIZED", status: 401},
		{name: "unauthorized empty", err: NewUnauthorizedError(""), code: "UNAUTHORIZED", status: 401},
		{name: "forbidden", err: NewForbiddenError("admin only"), code: "FORBIDDEN", status: 403},
		{name: "forbidden empty", err: NewForbiddenError(""), code: "FORBIDDEN", status: 403},
		{name: "bad request", err: NewBadRequestError("invalid"), code: "BAD_REQUEST", status: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.err.Code != tt.code || tt.err.HTTPStatus != tt.status || tt.err.Error() == "" {
				t.Fatalf("error = %#v", tt.err)
			}
		})
	}
	joined := Join(stderrors.New("one"), stderrors.New("two"))
	if joined == nil {
		t.Fatal("Join() returned nil")
	}
}

func TestWithDetail_ConcurrentCallsAreIsolated(t *testing.T) {
	base := New("TEST", "test", http.StatusBadRequest)
	const workers = 32

	results := make(chan *AppError, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- base.WithDetail("worker", i)
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[int]bool, workers)
	for result := range results {
		worker, ok := result.Details["worker"].(int)
		if !ok {
			t.Fatalf("worker detail has type %T", result.Details["worker"])
		}
		seen[worker] = true
	}
	if len(seen) != workers {
		t.Fatalf("isolated results = %d, want %d", len(seen), workers)
	}
	if len(base.Details) != 0 {
		t.Fatalf("base details = %v, want empty", base.Details)
	}
}

func TestErrorHandler_DoesNotExposePanic(t *testing.T) {
	handler := ErrorHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database-password-secret")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/panic", nil)

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if strings.Contains(w.Body.String(), "database-password-secret") || strings.Contains(w.Body.String(), "panic") {
		t.Fatalf("response exposes panic: %s", w.Body.String())
	}
	var response ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("code = %q, want INTERNAL_ERROR", response.Error.Code)
	}
}
