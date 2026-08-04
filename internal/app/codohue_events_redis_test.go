package app

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/jarviisha/darkvoid/pkg/config"
	"github.com/jarviisha/darkvoid/pkg/logger"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
)

// Output is discarded rather than left on stdout: one of these cases deliberately
// probes an unreachable host, and its warning is expected output, not a failure.
func testApp(cfg *config.Config) *Application {
	return &Application{
		cfg: cfg,
		log: logger.New(&logger.Config{Level: "error", Format: "json", Output: io.Discard}),
	}
}

// codohueEventsClient is the whole point of the split: the producer must be able
// to reach Codohue's Redis while the cache, the timeline store, and the
// notification pub/sub stay on ours. If this ever returned app.redis when a
// dedicated client exists, events would go to the wrong server and Codohue's
// consumer would never see them — silently, since XADD would still succeed.

func TestCodohueEventsClient_PrefersTheDedicatedInstance(t *testing.T) {
	app := testApp(&config.Config{})
	app.redis = &pkgredis.Client{}
	app.codohueEvents = &pkgredis.Client{}

	if got := app.codohueEventsClient(); got != app.codohueEvents {
		t.Errorf("codohueEventsClient() returned the app cache client; want the dedicated events client")
	}
}

func TestCodohueEventsClient_FallsBackToTheAppRedis(t *testing.T) {
	app := testApp(&config.Config{})
	app.redis = &pkgredis.Client{}

	if got := app.codohueEventsClient(); got != app.redis {
		t.Errorf("codohueEventsClient() = %p, want the app redis client %p", got, app.redis)
	}
}

func TestSetupCodohueEventsRedis_NoopWithoutAConfiguredHost(t *testing.T) {
	app := testApp(&config.Config{
		Codohue: config.CodohueConfig{Enabled: true},
	})

	app.setupCodohueEventsRedis(context.Background())

	if app.codohueEvents != nil {
		t.Errorf("codohueEvents = %p, want nil so the producer reuses the app's Redis", app.codohueEvents)
	}
}

// A dedicated events Redis is pointless while the integration is off, and dialing
// it would open a connection pool nothing ever writes to.
func TestSetupCodohueEventsRedis_SkippedWhenCodohueDisabled(t *testing.T) {
	app := testApp(&config.Config{
		Codohue: config.CodohueConfig{
			Enabled: false,
			EventsRedis: config.CodohueEventsRedisConfig{
				Enabled:     true,
				RedisConfig: config.RedisConfig{Host: "codohue-redis", Port: 6379, PoolSize: 5},
			},
		},
	})

	app.setupCodohueEventsRedis(context.Background())

	if app.codohueEvents != nil {
		t.Errorf("codohueEvents = %p, want nil when CODOHUE_ENABLED is false", app.codohueEvents)
	}
}

// The behaviour this method exists to guarantee: an events Redis that is down at
// boot must not fail boot, and must not be discarded. Dropping the client would
// disable behavior events for the life of the process — the same mistake already
// fixed for the Codohue HTTP client's boot probe. The address below is reserved
// for documentation examples (RFC 5737) and does not resolve.
func TestSetupCodohueEventsRedis_KeepsTheClientWhenUnreachable(t *testing.T) {
	app := testApp(&config.Config{
		Codohue: config.CodohueConfig{
			Enabled: true,
			EventsRedis: config.CodohueEventsRedisConfig{
				Enabled: true,
				RedisConfig: config.RedisConfig{
					Host:     "192.0.2.1",
					Port:     6379,
					PoolSize: 5,
				},
			},
		},
	})

	// HealthCheck derives its deadline from this context, so a short one keeps the
	// test off the 5s dial timeout without changing what is being asserted.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	app.setupCodohueEventsRedis(ctx)

	if app.codohueEvents == nil {
		t.Fatalf("codohueEvents = nil after an unreachable probe; want the client kept so it can reconnect")
	}
	t.Cleanup(func() { _ = app.codohueEvents.Close() })

	if got := app.codohueEventsClient(); got != app.codohueEvents {
		t.Errorf("codohueEventsClient() did not return the kept client")
	}
}
