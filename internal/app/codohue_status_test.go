package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jarviisha/darkvoid/pkg/logger"
)

func TestCodohueStatus_DefaultsToOff(t *testing.T) {
	state, reason := newCodohueStatus().get()
	if state != CodohueOff {
		t.Errorf("state = %q, want %q", state, CodohueOff)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}

// A degraded state carries why. Recovering must clear it, or /health would keep
// reporting a stale reason next to an active state.
func TestCodohueStatus_RecoveryClearsTheReason(t *testing.T) {
	s := newCodohueStatus()

	s.set(CodohueDegraded, "dial tcp: lookup codohue-admin: server misbehaving")
	state, reason := s.get()
	if state != CodohueDegraded || reason == "" {
		t.Fatalf("state = %q, reason = %q, want degraded with a reason", state, reason)
	}

	s.set(CodohueActive, "")
	state, reason = s.get()
	if state != CodohueActive {
		t.Errorf("state = %q, want %q", state, CodohueActive)
	}
	if reason != "" {
		t.Errorf("reason = %q, want it cleared on recovery", reason)
	}
}

// The monitor writes this while request goroutines read it. Run under -race,
// this is what proves the atomic is load-bearing rather than decorative.
func TestCodohueStatus_ConcurrentReadsAndWrites(t *testing.T) {
	s := newCodohueStatus()
	var wg sync.WaitGroup

	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				s.set(CodohueDegraded, "unreachable")
				s.set(CodohueActive, "")
			}
		}()
	}
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_, _ = s.get()
			}
		}()
	}
	wg.Wait()
}

// health has to stay 200 while Codohue is degraded: the recommender is auxiliary,
// and answering 503 would pull a working API out of load-balancer rotation over a
// feature the feed already falls back from.
func TestHealthCheck_DegradedCodohueIsStillHealthy(t *testing.T) {
	status := newCodohueStatus()
	status.set(CodohueDegraded, "connection refused")
	s := &Server{log: logger.New(nil), codohue: status}

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil)
	s.healthCheckHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — a degraded recommender is not an unhealthy API", w.Code)
	}

	var body HealthCheckResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "healthy" {
		t.Errorf("status = %q, want healthy", body.Status)
	}
	if body.Codohue != CodohueDegraded {
		t.Errorf("codohue = %q, want %q", body.Codohue, CodohueDegraded)
	}
	if body.CodohueReason != "connection refused" {
		t.Errorf("codohue_reason = %q, want the failure surfaced to a monitor", body.CodohueReason)
	}
}

func TestHealthCheck_ActiveCodohueOmitsTheReason(t *testing.T) {
	status := newCodohueStatus()
	status.set(CodohueActive, "")
	s := &Server{log: logger.New(nil), codohue: status}

	w := httptest.NewRecorder()
	s.healthCheckHandler(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil))

	var body HealthCheckResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Codohue != CodohueActive {
		t.Errorf("codohue = %q, want %q", body.Codohue, CodohueActive)
	}
	if body.CodohueReason != "" {
		t.Errorf("codohue_reason = %q, want empty when active", body.CodohueReason)
	}
}
