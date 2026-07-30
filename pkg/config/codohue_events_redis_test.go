package config

import "testing"

// The events Redis is the one connection in this config whose absence is a
// meaningful setting rather than a missing value: an unset host means "publish to
// the app's own Redis", not "misconfigured". These tests pin that reading, since
// getting it backwards would either drop every behavior event or dial localhost.

func TestLoadCodohueEventsRedis_UnsetHostMeansReuseAppRedis(t *testing.T) {
	cfg := loadCodohueEventsRedisConfig()

	if cfg.Enabled {
		t.Errorf("Enabled = true with no host set; want false so the producer reuses the app's Redis")
	}
	if cfg.Host != "" {
		t.Errorf("Host = %q, want empty", cfg.Host)
	}
}

func TestLoadCodohueEventsRedis_HostEnablesIt(t *testing.T) {
	t.Setenv("CODOHUE_EVENTS_REDIS_HOST", "codohue-redis")

	cfg := loadCodohueEventsRedisConfig()

	if !cfg.Enabled {
		t.Errorf("Enabled = false after naming a host; want true")
	}
	if cfg.Host != "codohue-redis" {
		t.Errorf("Host = %q, want %q", cfg.Host, "codohue-redis")
	}
	// Defaults must stand on their own: naming only the host is the common case,
	// and a zero port or pool would fail at dial time rather than here.
	if cfg.Port != 6379 {
		t.Errorf("Port = %d, want 6379", cfg.Port)
	}
	if cfg.DB != 0 {
		t.Errorf("DB = %d, want 0", cfg.DB)
	}
	if cfg.PoolSize < 1 {
		t.Errorf("PoolSize = %d, want at least 1", cfg.PoolSize)
	}
}

func TestLoadCodohueEventsRedis_OverridesAreRead(t *testing.T) {
	t.Setenv("CODOHUE_EVENTS_REDIS_HOST", "events.internal")
	t.Setenv("CODOHUE_EVENTS_REDIS_PORT", "6380")
	t.Setenv("CODOHUE_EVENTS_REDIS_PASSWORD", "s3cret")
	t.Setenv("CODOHUE_EVENTS_REDIS_DB", "3")

	cfg := loadCodohueEventsRedisConfig()

	if cfg.Port != 6380 {
		t.Errorf("Port = %d, want 6380", cfg.Port)
	}
	if cfg.Password != "s3cret" {
		t.Errorf("Password = %q, want %q", cfg.Password, "s3cret")
	}
	if cfg.DB != 3 {
		t.Errorf("DB = %d, want 3", cfg.DB)
	}
}

// The app's own REDIS_* vars must not leak into the events connection. They are
// different servers in the deployment this option exists for, so inheriting the
// app's host would silently undo the split.
func TestLoadCodohueEventsRedis_DoesNotInheritAppRedisVars(t *testing.T) {
	t.Setenv("REDIS_HOST", "darkvoid-redis")
	t.Setenv("REDIS_PORT", "6390")
	t.Setenv("REDIS_PASSWORD", "app-password")
	t.Setenv("REDIS_DB", "7")

	cfg := loadCodohueEventsRedisConfig()

	if cfg.Enabled {
		t.Errorf("Enabled = true from REDIS_HOST alone; the events host must be named explicitly")
	}
	if cfg.Host == "darkvoid-redis" {
		t.Errorf("Host inherited REDIS_HOST")
	}
	if cfg.Port == 6390 {
		t.Errorf("Port inherited REDIS_PORT")
	}
	if cfg.Password == "app-password" {
		t.Errorf("Password inherited REDIS_PASSWORD")
	}
	if cfg.DB == 7 {
		t.Errorf("DB inherited REDIS_DB")
	}
}

func TestLoadCodohueConfig_CarriesEventsRedis(t *testing.T) {
	t.Setenv("CODOHUE_ENABLED", "true")
	t.Setenv("CODOHUE_EVENTS_REDIS_HOST", "codohue-redis")

	cfg := loadCodohueConfig()

	if !cfg.EventsRedis.Enabled || cfg.EventsRedis.Host != "codohue-redis" {
		t.Errorf("EventsRedis = %+v, want enabled with host %q", cfg.EventsRedis, "codohue-redis")
	}
}
