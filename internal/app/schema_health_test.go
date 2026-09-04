package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type schemaProbeStub struct {
	pending []string
	err     error
}

func (s schemaProbeStub) PendingModules(context.Context) ([]string, error) {
	return s.pending, s.err
}

func healthResponse(t *testing.T, server *Server) (int, HealthCheckResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	server.healthCheckHandler(rec, req)

	var got HealthCheckResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return rec.Code, got
}

// TestHealthCheck_UnhealthyWhenAModuleIsBehind is the signal that was missing.
//
// Connectivity probes answer "can I reach the database", never "does it hold the
// tables this binary was built against". A deployment that skipped a module's
// migrations therefore reported itself healthy and took traffic: with
// settings.feed absent the feed ranks on compiled defaults while the admin API
// fails, and with usr.feed_outbox absent the consumer drains nothing, so
// followers' timelines quietly stop being updated. Neither surfaced anywhere but
// a log line.
//
// Migrations run before the app in every deployment path here, so a module that
// is behind means the rollout is broken rather than merely early — which is why
// it takes the instance out of rotation instead of only being reported.
func TestHealthCheck_UnhealthyWhenAModuleIsBehind(t *testing.T) {
	server := testServer(nil)
	server.schema = schemaProbeStub{pending: []string{"settings", "post"}}

	code, got := healthResponse(t, server)

	if code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d so a rollout missing migrations is taken out of rotation", code, http.StatusServiceUnavailable)
	}
	if got.Status != "unhealthy" {
		t.Errorf("status = %q, want %q", got.Status, "unhealthy")
	}
	if got.Schema != "behind" {
		t.Errorf("schema = %q, want %q", got.Schema, "behind")
	}
	for _, module := range []string{"settings", "post"} {
		if !strings.Contains(got.SchemaReason, module) {
			t.Errorf("schema_reason %q does not name the %s module", got.SchemaReason, module)
		}
	}
	// The database answered; naming it as a casualty would send the operator
	// looking at connectivity for a migration problem.
	if got.Database != "up" {
		t.Errorf("database = %q, want %q", got.Database, "up")
	}
}

// TestHealthCheck_SchemaProbeFailureDoesNotUnseatTheInstance separates "the
// schema is behind" from "the check itself did not run". Connectivity is already
// covered by the database probe, so failing here would take a working deployment
// out of rotation over the checker rather than over the thing checked.
func TestHealthCheck_SchemaProbeFailureDoesNotUnseatTheInstance(t *testing.T) {
	server := testServer(nil)
	server.schema = schemaProbeStub{err: errors.New("relation \"schema_migrations_post\" does not exist")}

	code, got := healthResponse(t, server)

	if code != http.StatusOK {
		t.Errorf("status = %d, want %d — a probe failure is not a reason to unseat the instance", code, http.StatusOK)
	}
	if got.Schema != "unknown" {
		t.Errorf("schema = %q, want %q", got.Schema, "unknown")
	}
}
