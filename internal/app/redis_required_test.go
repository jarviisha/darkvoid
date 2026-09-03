package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jarviisha/darkvoid/pkg/config"
	"github.com/jarviisha/darkvoid/pkg/logger"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
)

type storageHealthStub struct{ err error }

func (s storageHealthStub) HealthCheck(context.Context) error { return s.err }

// Redis stopped being optional: it holds the feed cache, the materialized
// timeline and the notification Pub/Sub, so an instance that cannot reach one
// does not serve a slower feed — it serves a different one. These tests pin the
// two places that has to be visible: boot refuses, and /health says so.

// closedAddr returns an address nothing is listening on, so a dial fails with
// "connection refused" immediately. An unroutable address would test the same
// thing but spend the dial timeout doing it.
func closedAddr(t *testing.T) (string, int) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr.IP.String(), addr.Port
}

func TestSetupRedis_FailsBootWhenUnreachable(t *testing.T) {
	host, port := closedAddr(t)
	app := testApp(&config.Config{
		Redis: config.RedisConfig{Host: host, Port: port, PoolSize: 1},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := app.setupRedis(ctx); err == nil {
		t.Fatalf("setupRedis() = nil with no server listening; want an error so boot fails")
	}
	if app.redis != nil {
		t.Errorf("app.redis = %p after a failed dial; want nil so nothing downstream sees a half-built client", app.redis)
	}
}

// The dedicated events Redis keeps its old behaviour on purpose — it is the one
// Redis that is genuinely optional, and dropping behavior events is not worth
// refusing to serve the API over. This pins the two apart.
func TestSetupRedis_UnlikeTheCodohueEventsRedis_IsFatal(t *testing.T) {
	host, port := closedAddr(t)
	app := testApp(&config.Config{
		Codohue: config.CodohueConfig{
			Enabled: true,
			EventsRedis: config.CodohueEventsRedisConfig{
				Enabled:     true,
				RedisConfig: config.RedisConfig{Host: host, Port: port, PoolSize: 1},
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	app.setupCodohueEventsRedis(ctx)

	if app.codohueEvents == nil {
		t.Fatalf("codohueEvents = nil after an unreachable probe; the events Redis must stay optional")
	}
	t.Cleanup(func() { _ = app.codohueEvents.Close() })
}

func testServer(redis *pkgredis.Client) *Server {
	return &Server{
		log:   logger.New(&logger.Config{Level: "error", Format: "json", Output: io.Discard}),
		redis: redis,
	}
}

func TestHealthCheck_UnhealthyWhenRedisIsDown(t *testing.T) {
	host, port := closedAddr(t)
	client := pkgredis.NewLazy(&pkgredis.Config{Host: host, Port: port, PoolSize: 1})
	t.Cleanup(func() { _ = client.Close() })

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	testServer(client).healthCheckHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d so a load balancer takes the instance out of rotation", rec.Code, http.StatusServiceUnavailable)
	}

	var got HealthCheckResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Status != "unhealthy" {
		t.Errorf("status = %q, want %q", got.Status, "unhealthy")
	}
	if got.Redis != "down" {
		t.Errorf("redis = %q, want %q", got.Redis, "down")
	}
	// The database was never probed here, and must not be reported as a casualty
	// of Redis being down — the point of naming both is telling them apart.
	if got.Database != "up" {
		t.Errorf("database = %q, want %q", got.Database, "up")
	}
}

func TestHealthCheck_UnhealthyWhenStorageIsDown(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	server := testServer(nil)
	server.storage = storageHealthStub{err: errors.New("bucket unavailable")}
	server.healthCheckHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var got HealthCheckResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Status != "unhealthy" || got.Storage != "down" {
		t.Fatalf("health = %+v, want unhealthy storage down", got)
	}
}
