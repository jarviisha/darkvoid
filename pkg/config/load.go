package config

import (
	"strings"
	"time"
)

// loadAppConfig loads application configuration
func loadAppConfig() AppConfig {
	return AppConfig{
		Name:        getEnv("SERVICE_NAME", "darkvoid"),
		Version:     getEnv("SERVICE_VERSION", "1.0.0"),
		Environment: getEnv("ENVIRONMENT", "development"),
	}
}

// loadDatabaseConfig loads database configuration
func loadDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		Host:            getEnv("DB_HOST", "localhost"),
		Port:            getEnvInt("DB_PORT", 5432),
		User:            getEnv("DB_USER", "postgres"),
		Password:        getEnv("DB_PASSWORD", "postgres"),
		Database:        getEnv("DB_NAME", "darkvoid"),
		SSLMode:         getEnv("DB_SSLMODE", "disable"),
		MaxConns:        int32(getEnvInt("DB_MAX_CONNS", 25)), //nolint:gosec // env config values are small
		MinConns:        int32(getEnvInt("DB_MIN_CONNS", 5)),  //nolint:gosec // env config values are small
		MaxConnLifetime: getEnvDuration("DB_MAX_CONN_LIFETIME", time.Hour),
		MaxConnIdleTime: getEnvDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
	}
}

// loadLoggerConfig loads logger configuration
func loadLoggerConfig() LoggerConfig {
	return LoggerConfig{
		Level:     getEnv("LOG_LEVEL", "info"),
		Format:    getEnv("LOG_FORMAT", "json"),
		AddSource: getEnvBool("LOG_ADD_SOURCE", false),
	}
}

// loadServerConfig loads server configuration
func loadServerConfig() ServerConfig {
	return ServerConfig{
		Host:              getEnv("SERVER_HOST", "0.0.0.0"),
		Port:              getEnvInt("SERVER_PORT", 8080),
		ReadTimeout:       getEnvDuration("SERVER_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:      getEnvDuration("SERVER_WRITE_TIMEOUT", 10*time.Second),
		IdleTimeout:       getEnvDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
		RequestTimeout:    getEnvDuration("SERVER_REQUEST_TIMEOUT", 60*time.Second),
		AllowedOrigins:    getEnvSlice("CORS_ALLOWED_ORIGINS", []string{"*"}),
		RateLimitRequests: getEnvInt("RATE_LIMIT_REQUESTS", 100),
		RateLimitWindow:   getEnvDuration("RATE_LIMIT_WINDOW", 1*time.Minute),
	}
}

// loadCookieConfig loads refresh token cookie configuration.
//
// secureDefault is a parameter rather than another getEnv call because
// COOKIE_SECURE is a tri-state: unset means "follow the deployment
// environment", and only the already-loaded AppConfig knows which that is.
//
// SameSite is lowercased and trimmed before Validate sees it, so that Lax and a
// stray trailing space are accepted rather than rejected as typos. Anything
// still unrecognised after that is a real mistake and fails the boot.
func loadCookieConfig(secureDefault bool) CookieConfig {
	return CookieConfig{
		SameSite: strings.ToLower(strings.TrimSpace(getEnv("COOKIE_SAMESITE", "lax"))),
		Domain:   strings.TrimSpace(getEnv("COOKIE_DOMAIN", "")),
		Secure:   getEnvBool("COOKIE_SECURE", secureDefault),
	}
}

// loadStorageConfig loads storage configuration
func loadStorageConfig() StorageConfig {
	return StorageConfig{
		Provider: getEnv("STORAGE_PROVIDER", "local"),
		BaseURL:  getEnv("STORAGE_BASE_URL", "http://localhost:8080/static"),
		LocalDir: getEnv("STORAGE_LOCAL_DIR", "./uploads"),

		S3Endpoint:  getEnv("STORAGE_S3_ENDPOINT", ""),
		S3Bucket:    getEnv("STORAGE_S3_BUCKET", "darkvoid"),
		S3Region:    getEnv("STORAGE_S3_REGION", "us-east-1"),
		S3AccessKey: getEnv("STORAGE_S3_ACCESS_KEY", ""),
		S3SecretKey: getEnv("STORAGE_S3_SECRET_KEY", ""),
		S3UseSSL:    getEnvBool("STORAGE_S3_USE_SSL", false),
	}
}

// loadJWTConfig loads JWT configuration
func loadJWTConfig() JWTConfig {
	return JWTConfig{
		Secret:            getEnv("JWT_SECRET", ""),
		Issuer:            getEnv("JWT_ISSUER", "darkvoid"),
		AccessTokenExpiry: getEnvDuration("JWT_ACCESS_TOKEN_EXPIRY", 15*time.Minute),
	}
}

// loadRefreshTokenConfig loads refresh token configuration
func loadRefreshTokenConfig() RefreshTokenConfig {
	return RefreshTokenConfig{
		Expiry: getEnvDuration("REFRESH_TOKEN_EXPIRY", 7*24*time.Hour),
	}
}

// loadRootConfig loads root bootstrap configuration.
// ROOT_EMAIL and ROOT_PASSWORD must both be set to enable auto-bootstrap.
func loadRootConfig() RootConfig {
	return RootConfig{
		Email:       getEnv("ROOT_EMAIL", ""),
		Password:    getEnv("ROOT_PASSWORD", ""),
		Username:    getEnv("ROOT_USERNAME", "root"),
		DisplayName: getEnv("ROOT_DISPLAY_NAME", "Root Admin"),
	}
}

// loadCodohueConfig loads Codohue personalization service configuration from environment variables.
// Set CODOHUE_ENABLED=true to enable CF recommendations and behavior event tracking.
//
//	CODOHUE_ENABLED        (default: false)
//	CODOHUE_BASE_URL       (default: "") — data-plane API (cmd/api)
//	CODOHUE_ADMIN_URL      (default: "") — admin plane (cmd/admin); required for namespace provisioning
//	CODOHUE_NAMESPACE_KEY  (default: "") — namespace key from one-time namespace creation
//	CODOHUE_ADMIN_KEY      (default: CODOHUE_API_KEY) — admin key for namespace provisioning only
//	CODOHUE_NAMESPACE      (default: "darkvoid_feed")
//	CODOHUE_EMBEDDING_DIM  (default: 64) — dim of Codohue's catalog embedder: 64, 128, 256 or 512
//
// The codohue:events stream can live on a Redis other than the app's own. Leave
// the host unset to publish to the app's Redis — see CodohueConfig.EventsRedis.
//
//	CODOHUE_EVENTS_REDIS_HOST     (default: "") — unset means reuse the app's Redis
//	CODOHUE_EVENTS_REDIS_PORT     (default: 6379)
//	CODOHUE_EVENTS_REDIS_PASSWORD (default: "")
//	CODOHUE_EVENTS_REDIS_DB       (default: 0)
func loadCodohueConfig() CodohueConfig {
	return CodohueConfig{
		Enabled:      getEnvBool("CODOHUE_ENABLED", false),
		BaseURL:      getEnv("CODOHUE_BASE_URL", ""),
		AdminURL:     getEnv("CODOHUE_ADMIN_URL", ""),
		NamespaceKey: getEnv("CODOHUE_NAMESPACE_KEY", ""),
		AdminKey:     getEnv("CODOHUE_ADMIN_KEY", getEnv("CODOHUE_API_KEY", "")),
		Namespace:    getEnv("CODOHUE_NAMESPACE", "darkvoid_feed"),
		EmbeddingDim: getEnvInt("CODOHUE_EMBEDDING_DIM", 64),
		EventsRedis:  loadCodohueEventsRedisConfig(),
	}
}

// loadCodohueEventsRedisConfig loads the connection to the Redis holding the
// codohue:events stream. Enabled is derived rather than read from its own
// variable: a separate events Redis is exactly the case where a host was named,
// so a CODOHUE_EVENTS_REDIS_ENABLED flag could only ever disagree with the host
// it was supposed to describe.
//
// PoolSize is deliberately not configurable. This client publishes to one stream
// and does nothing else, so it needs far fewer connections than the cache — and a
// knob whose only correct value is "small" is a knob that gets set wrong.
func loadCodohueEventsRedisConfig() CodohueEventsRedisConfig {
	host := getEnv("CODOHUE_EVENTS_REDIS_HOST", "")
	return CodohueEventsRedisConfig{
		Enabled: host != "",
		RedisConfig: RedisConfig{
			Host:     host,
			Port:     getEnvInt("CODOHUE_EVENTS_REDIS_PORT", 6379),
			Password: getEnv("CODOHUE_EVENTS_REDIS_PASSWORD", ""),
			DB:       getEnvInt("CODOHUE_EVENTS_REDIS_DB", 0),
			PoolSize: 5,
		},
	}
}

// loadMailerConfig loads mailer configuration from environment variables.
// Set MAILER_PROVIDER=resend or smtp to enable real email delivery.
//
//	MAILER_PROVIDER (default: nop)
//	MAILER_HOST     (default: "")
//	MAILER_PORT     (default: 587)
//	MAILER_USERNAME (default: "")
//	MAILER_PASSWORD (default: "")
//	RESEND_API_KEY  (default: "")
//	RESEND_WEBHOOK_SECRET (default: "")
//	MAILER_FROM     (default: "noreply@darkvoid.app")
//	MAILER_BASE_URL (default: "http://localhost:3000")
//
// RESEND_API_KEY keeps the vendor's own name rather than a MAILER_ prefix: it is
// pasted straight from the provider's dashboard, and renaming it only makes that
// harder to match up.
func loadMailerConfig() MailerConfig {
	return MailerConfig{
		Provider:      getEnv("MAILER_PROVIDER", "nop"),
		Host:          getEnv("MAILER_HOST", ""),
		Port:          getEnvInt("MAILER_PORT", 587),
		Username:      getEnv("MAILER_USERNAME", ""),
		Password:      getEnv("MAILER_PASSWORD", ""),
		APIKey:        getEnv("RESEND_API_KEY", ""),
		WebhookSecret: getEnv("RESEND_WEBHOOK_SECRET", ""),
		From:          getEnv("MAILER_FROM", "noreply@darkvoid.app"),
		BaseURL:       getEnv("MAILER_BASE_URL", "http://localhost:3000"),
	}
}

// loadRedisConfig loads Redis configuration from environment variables.
//
// There is no REDIS_ENABLED. Redis is required — the app refuses to boot without
// one, the same as with Postgres. The defaults describe a local server so a
// developer with the compose stack up needs to set nothing.
//
//	REDIS_HOST      (default: localhost)
//	REDIS_PORT      (default: 6379)
//	REDIS_PASSWORD  (default: "")
//	REDIS_DB        (default: 0)
//	REDIS_POOL_SIZE (default: 10)
func loadRedisConfig() RedisConfig {
	return RedisConfig{
		Host:     getEnv("REDIS_HOST", "localhost"),
		Port:     getEnvInt("REDIS_PORT", 6379),
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       getEnvInt("REDIS_DB", 0),
		PoolSize: getEnvInt("REDIS_POOL_SIZE", 10),
	}
}

// loadFeedFanoutConfig loads the two fanout knobs that size the in-process worker
// pool at construction.
//
//	FEED_FANOUT_WORKERS    (default: 10)
//	FEED_FANOUT_QUEUE_SIZE (default: 10000)
//
// The rest of the old FEED_* group — FEED_TIMELINE_ENABLED,
// FEED_TIMELINE_ROLLOUT_PERCENT, FEED_TIMELINE_MAX_ITEMS, FEED_TIMELINE_TTL,
// FEED_TIMELINE_REFRESH_ON_MISS, FEED_FANOUT_ENABLED and
// FEED_FANOUT_MAX_FOLLOWERS — is no longer read here. Those live in settings.feed
// and are edited through PATCH /admin/settings/feed, so setting them in the
// environment now does nothing. See migrations/settings/000002 for why these two
// stayed behind.
func loadFeedFanoutConfig() FeedFanoutConfig {
	return FeedFanoutConfig{
		Workers:   getEnvInt("FEED_FANOUT_WORKERS", 10),
		QueueSize: getEnvInt("FEED_FANOUT_QUEUE_SIZE", 10000),
	}
}

// loadSettingsConfig loads how often database-stored settings are re-read.
//
//	SETTINGS_REFRESH_INTERVAL (default: 30s)
//
// 30 seconds is chosen against what these settings are for: an operator watching
// a graph after nudging a rollout wants the effect inside a minute, and the cost
// is one indexed one-row SELECT per instance per interval. The instance serving
// the PATCH does not wait for a tick — this only bounds how far behind its
// siblings can be.
func loadSettingsConfig() SettingsConfig {
	return SettingsConfig{
		RefreshInterval: getEnvDuration("SETTINGS_REFRESH_INTERVAL", 30*time.Second),
	}
}
