package config

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application configuration
type Config struct {
	App          AppConfig
	Database     DatabaseConfig
	Logger       LoggerConfig
	Server       ServerConfig
	Cookie       CookieConfig
	JWT          JWTConfig
	RefreshToken RefreshTokenConfig
	Storage      StorageConfig
	Root         RootConfig
	Redis        RedisConfig
	FeedFanout   FeedFanoutConfig
	Settings     SettingsConfig
	Codohue      CodohueConfig
	Mailer       MailerConfig
}

// CodohueConfig holds configuration for the Codohue personalization service.
// Set CODOHUE_ENABLED=true to enable collaborative-filtering recommendations and event tracking.
// When disabled, the feed uses only local scoring without CF augmentation.
//
// Auth model (two-tier):
//   - NamespaceKey (CODOHUE_NAMESPACE_KEY): used for all runtime endpoints (events, recommendations, rank, trending, delete).
//   - AdminKey     (CODOHUE_ADMIN_KEY):     used only for namespace provisioning via the admin plane
//     (session login + PUT /api/admin/v1/namespaces/{ns} on AdminURL).
type CodohueConfig struct {
	Enabled      bool   // enable Codohue integration
	BaseURL      string // data-plane HTTP base URL (cmd/api), e.g. "http://codohue-host:2001"
	AdminURL     string // admin-plane HTTP base URL (cmd/admin), e.g. "http://codohue-host:2002"; required for provisioning
	NamespaceKey string // namespace key — returned once on namespace creation; used for all API calls
	AdminKey     string // admin key — only for namespace provisioning, not used in the request path
	Namespace    string // namespace identifier for this app's events and recommendations
	EmbeddingDim int    // dimension Codohue's catalog embedder produces; must be one of 64/128/256/512

	// EventsRedis addresses the Redis that carries the codohue:events stream.
	// Behavior events are the one part of this integration that does not travel
	// over HTTP: Codohue's consumer reads the stream from its own Redis, so
	// darkvoid has to XADD into that same instance for events to arrive at all.
	//
	// Enabled false (CODOHUE_EVENTS_REDIS_HOST unset) means the stream lives on
	// the app's own Redis, and the producer reuses that client. That is the right
	// default: it is correct whenever one Redis serves both sides, and it keeps
	// the deployments that already do so working untouched.
	//
	// Set it when Codohue owns a separate Redis. Pointing the app's REDIS_HOST at
	// Codohue's instance also works, but it drags the feed cache, the timeline
	// store, and the notification pub/sub onto a server darkvoid does not own —
	// so an outage in an integration that is meant to be optional takes core
	// features down with it.
	EventsRedis CodohueEventsRedisConfig
}

// RedisConfig holds Redis connection configuration.
//
// There is no Enabled flag. Redis is a hard dependency of the API: the feed
// cache, the materialized timeline and the cross-instance notification pub/sub
// all live in it, so an instance without one does not serve a degraded feed —
// it serves a different one. The no-op caches that used to stand in made that
// difference invisible, which is the failure this removal is about.
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

// CodohueEventsRedisConfig is the one Redis connection that stays optional, and
// it is optional in a different sense: absent means "the stream lives on the
// app's Redis", not "there is no stream".
type CodohueEventsRedisConfig struct {
	RedisConfig

	// Enabled reports that a host was named, i.e. that Codohue owns a Redis of
	// its own. Derived, never read from its own variable — see
	// loadCodohueEventsRedisConfig.
	Enabled bool
}

// FeedFanoutConfig is what is left of the old FEED_* group after the rest moved
// into settings.feed: the two knobs that size the in-process fanout machinery.
//
// They stayed in the environment because they are allocated once, at
// construction — Workers starts that many goroutines and QueueSize fixes the
// channel's capacity. A stored value for either would present a knob that appears
// to change something and does not, until the next restart, which is a worse
// deal than a variable that is honest about needing one.
//
// Everything else the feed does — whether timelines are served, to whom, how many
// entries they hold, whether fanout runs at all, and the three ranking weights —
// is in settings.feed and editable through PATCH /admin/settings/feed.
type FeedFanoutConfig struct {
	Workers   int
	QueueSize int
}

// SettingsConfig configures how the process reads its database-stored settings.
//
// This one cannot itself live in the database: it is the setting that says how
// often to read the settings, and a stored value would only take effect after a
// read performed at the interval it was trying to change.
type SettingsConfig struct {
	// RefreshInterval is how often each instance re-reads settings.feed. It bounds
	// how long a change made through the admin API takes to reach an instance that
	// did not serve the request — the one that did applies it immediately.
	RefreshInterval time.Duration
}

// RootConfig holds bootstrap configuration for the initial root/admin account.
// When ROOT_EMAIL and ROOT_PASSWORD are set, the app auto-creates a root user on
// first startup if no users exist yet.
type RootConfig struct {
	// Email is the root user's email address. Leave empty to disable auto-bootstrap.
	Email string
	// Password is the root user's initial plaintext password.
	// It is only used during bootstrap; the value is never stored.
	Password string
	// Username is the root user's login username (default: "root").
	Username string
	// DisplayName is the root user's display name (default: "Root Admin").
	DisplayName string
}

// StorageConfig holds file storage configuration
type StorageConfig struct {
	// Provider selects the storage backend: "local" for development or "s3"
	// for shared production object storage.
	Provider string

	// BaseURL is the public base URL used to build file URLs from keys.
	// e.g. "http://localhost:8080/static" or "https://cdn.example.com"
	BaseURL string

	// Local provider settings
	LocalDir string // e.g. "./uploads"

	// S3 contains AWS S3 or S3-compatible provider settings.
	S3 S3StorageConfig
}

// S3StorageConfig configures shared object storage. Endpoint is empty for AWS
// S3 and set for compatible providers such as MinIO. Empty static credentials
// use the AWS default credential chain.
type S3StorageConfig struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	UsePathStyle    bool
}

// MailerConfig holds email sending configuration.
// Set MAILER_PROVIDER=resend or smtp to enable real email delivery.
// When set to "nop" (default), emails are logged but not sent.
//
// Host/Port/Username/Password are SMTP-only; APIKey is resend-only. Each
// provider ignores the other's fields rather than the two sharing a field that
// means different things.
type MailerConfig struct {
	Provider string // "resend", "smtp" or "nop"
	Host     string
	Port     int
	Username string
	Password string
	APIKey   string // Resend API key, from RESEND_API_KEY
	// WebhookSecret is the Resend webhook signing secret ("whsec_..."). Empty
	// leaves the webhook route unregistered — see MailerConfig usage in
	// internal/app: an unverified endpoint that writes to the suppression list
	// would let anyone block any address.
	WebhookSecret string
	From          string
	BaseURL       string // application URL for building links in emails
}

// AppConfig holds application-level configuration
type AppConfig struct {
	Name        string
	Version     string
	Environment string // development, staging, production
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	SSLMode         string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// LoggerConfig holds logger configuration
type LoggerConfig struct {
	Level     string // debug, info, warn, error
	Format    string // json, text
	AddSource bool
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Host              string
	Port              int
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	RequestTimeout    time.Duration // Request timeout for middleware
	AllowedOrigins    []string      // CORS allowed origins
	TrustedProxyCIDRs []string      // Network peers allowed to supply client IP headers
	RateLimitRequests int           // Rate limit: requests per window
	RateLimitWindow   time.Duration // Rate limit: time window
}

// CookieConfig holds the deployment-dependent attributes of the refresh token
// cookie.
//
// Path and HttpOnly are deliberately absent. Path is coupled to where the auth
// routes are mounted, not to the deployment, so an environment variable for it
// would be a second source of truth that can disagree with the router — and it
// disagrees silently, as a cookie the browser simply never sends back. HttpOnly
// has only one safe value: a refresh token readable from JavaScript turns any
// XSS into long-lived session theft, and the clients that legitimately need the
// token in hand already ask for it with X-Client-Type: mobile.
type CookieConfig struct {
	// SameSite is "lax", "strict" or "none".
	//
	// Lax is correct while the frontend and the API share a registrable domain.
	// A frontend on a different domain needs "none": the browser withholds a Lax
	// cookie on cross-site fetches, so /auth/refresh answers 401 and reads as an
	// expired session rather than as a misconfiguration. CORS does not save it —
	// AllowCredentials governs whether the response is readable, not whether the
	// cookie was attached in the first place.
	SameSite string

	// Domain empty means a host-only cookie, which is what a single API host
	// wants. Set it to share the cookie across subdomains.
	Domain string

	// Secure defaults to "not development" and COOKIE_SECURE overrides it. The
	// override exists for the staging box that sits behind TLS termination while
	// still calling itself development, which the derived value gets wrong.
	Secure bool
}

// SameSiteMode maps the configured name onto http.SameSite.
//
// Validate has already rejected every name this does not recognise, so the
// fallback arm is unreachable in a running process. That rejection is the point:
// the zero http.SameSite is SameSiteDefaultMode, which emits no attribute at
// all, so a typo left to fall through would change behaviour without saying so.
func (c CookieConfig) SameSiteMode() http.SameSite {
	switch c.SameSite {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret            string
	Issuer            string
	Audience          string
	AccessTokenExpiry time.Duration
}

// RefreshTokenConfig holds refresh token configuration
type RefreshTokenConfig struct {
	Expiry time.Duration
}

// Load loads configuration from environment variables.
// It automatically loads .env file if present (silently ignored if not found).
func Load() (*Config, error) {
	// Load .env file if it exists — errors are silently ignored (e.g. production)
	_ = godotenv.Load()

	// App is loaded first because the cookie's Secure default is derived from
	// the environment name.
	app := loadAppConfig()

	cfg := &Config{
		App:          app,
		Database:     loadDatabaseConfig(),
		Logger:       loadLoggerConfig(),
		Server:       loadServerConfig(),
		Cookie:       loadCookieConfig(!isDevelopmentEnv(app.Environment)),
		JWT:          loadJWTConfig(),
		RefreshToken: loadRefreshTokenConfig(),
		Storage:      loadStorageConfig(),
		Root:         loadRootConfig(),
		Redis:        loadRedisConfig(),
		FeedFanout:   loadFeedFanoutConfig(),
		Settings:     loadSettingsConfig(),
		Codohue:      loadCodohueConfig(),
		Mailer:       loadMailerConfig(),
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate app config
	if c.App.Name == "" {
		return fmt.Errorf("app name is required")
	}

	// Validate database config
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if c.Database.Port == 0 {
		return fmt.Errorf("database port is required")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database user is required")
	}
	if c.Database.Database == "" {
		return fmt.Errorf("database name is required")
	}
	if c.Database.MaxConns < 1 {
		return fmt.Errorf("database max connections must be at least 1")
	}
	if c.Database.MinConns < 0 {
		return fmt.Errorf("database min connections cannot be negative")
	}
	if c.Database.MinConns > c.Database.MaxConns {
		return fmt.Errorf("database min connections cannot exceed max connections")
	}

	// Validate logger config
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.Logger.Level] {
		return fmt.Errorf("invalid log level: %s", c.Logger.Level)
	}

	validFormats := map[string]bool{"json": true, "text": true}
	if !validFormats[c.Logger.Format] {
		return fmt.Errorf("invalid log format: %s", c.Logger.Format)
	}

	// Validate server config
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}
	for _, cidr := range c.Server.TrustedProxyCIDRs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err)
		}
	}

	// Validate cookie config
	validSameSite := map[string]bool{"lax": true, "strict": true, "none": true}
	if !validSameSite[c.Cookie.SameSite] {
		return fmt.Errorf("invalid cookie samesite: %s (want lax, strict or none)", c.Cookie.SameSite)
	}
	// Browsers reject a SameSite=None cookie that is not also Secure. Left to
	// runtime this pair does not announce itself — the server sets a cookie, the
	// browser drops it, and every refresh fails as if the session had expired.
	if c.Cookie.SameSite == "none" && !c.Cookie.Secure {
		return fmt.Errorf("cookie samesite=none requires a secure cookie: set COOKIE_SECURE=true (ENVIRONMENT=%s implies false)", c.App.Environment)
	}
	// A scheme, port or path in the Domain attribute yields a cookie the browser
	// discards, so catch the shape here rather than in the network tab.
	if strings.ContainsAny(c.Cookie.Domain, ":/") {
		return fmt.Errorf("invalid cookie domain %q: use a bare hostname, without scheme, port or path", c.Cookie.Domain)
	}

	// Validate JWT config
	if c.JWT.Secret == "" {
		return fmt.Errorf("JWT secret is required")
	}
	if c.JWT.Issuer == "" {
		return fmt.Errorf("JWT issuer is required")
	}
	if c.JWT.Audience == "" {
		return fmt.Errorf("JWT audience is required")
	}
	if c.JWT.AccessTokenExpiry <= 0 {
		return fmt.Errorf("JWT access token expiry must be positive")
	}
	if c.RefreshToken.Expiry <= 0 {
		return fmt.Errorf("refresh token expiry must be positive")
	}
	if err := c.validateStorage(); err != nil {
		return err
	}
	if c.FeedFanout.Workers < 1 {
		return fmt.Errorf("feed fanout workers must be at least 1")
	}
	if c.FeedFanout.QueueSize < 1 {
		return fmt.Errorf("feed fanout queue size must be at least 1")
	}
	// The rollout percent, TTL and item-count checks that used to sit here moved
	// to entity.FeedSettingsUpdate.Validate and to the CHECKs on settings.feed —
	// the values are no longer read from the environment, so there is nothing left
	// here to validate. A bad value is now a 400 from the admin API instead of a
	// startup failure, which is the right trade for a knob that changes hourly.
	if c.Settings.RefreshInterval <= 0 {
		return fmt.Errorf("settings refresh interval must be positive")
	}

	return nil
}

func (c *Config) validateStorage() error {
	parsedBaseURL, err := url.Parse(c.Storage.BaseURL)
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" || (parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https") {
		return fmt.Errorf("storage base URL must be an absolute HTTP(S) URL")
	}

	isProduction := strings.EqualFold(strings.TrimSpace(c.App.Environment), "production")
	if isProduction {
		if c.Storage.Provider != "s3" {
			return fmt.Errorf("production storage provider must be s3 for shared multi-instance media")
		}
		if parsedBaseURL.Scheme != "https" {
			return fmt.Errorf("production storage base URL must use HTTPS")
		}
		hostname := parsedBaseURL.Hostname()
		if strings.EqualFold(hostname, "localhost") || isLoopbackHost(hostname) {
			return fmt.Errorf("production storage base URL must be publicly reachable, not %q", hostname)
		}
	}

	switch c.Storage.Provider {
	case "local":
		if strings.TrimSpace(c.Storage.LocalDir) == "" {
			return fmt.Errorf("storage local directory is required")
		}
	case "s3":
		if strings.TrimSpace(c.Storage.S3.Region) == "" {
			return fmt.Errorf("storage S3 region is required")
		}
		if strings.TrimSpace(c.Storage.S3.Bucket) == "" {
			return fmt.Errorf("storage S3 bucket is required")
		}
		if (c.Storage.S3.AccessKeyID == "") != (c.Storage.S3.SecretAccessKey == "") {
			return fmt.Errorf("storage S3 access key ID and secret access key must be set together")
		}
		if endpoint := strings.TrimSpace(c.Storage.S3.Endpoint); endpoint != "" {
			parsedEndpoint, endpointErr := url.Parse(endpoint)
			if endpointErr != nil || parsedEndpoint.Scheme == "" || parsedEndpoint.Host == "" || (parsedEndpoint.Scheme != "http" && parsedEndpoint.Scheme != "https") {
				return fmt.Errorf("storage S3 endpoint must be an absolute HTTP(S) URL")
			}
		}
	default:
		return fmt.Errorf("invalid storage provider %q: want local or s3", c.Storage.Provider)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// IsDevelopment checks if running in development environment
func (c *Config) IsDevelopment() bool {
	return isDevelopmentEnv(c.App.Environment)
}

// isDevelopmentEnv is the single definition of what counts as development.
// loadCookieConfig needs it to derive the Secure default before a Config exists,
// so it cannot be a method — and duplicating the comparison there would let the
// cookie disagree with IsDevelopment about which environment this is.
func isDevelopmentEnv(name string) bool {
	return name == "development"
}

// IsProduction checks if running in production environment
func (c *Config) IsProduction() bool {
	return c.App.Environment == "production"
}

// IsStaging checks if running in staging environment
func (c *Config) IsStaging() bool {
	return c.App.Environment == "staging"
}
