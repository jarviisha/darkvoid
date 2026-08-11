package errors

import (
	"context"
	"encoding/json"
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
