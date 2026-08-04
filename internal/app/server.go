package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/pkg/config"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
)

// Server wraps HTTP server
type Server struct {
	httpServer *http.Server
	router     *chi.Mux
	cfg        *config.Config
	log        *logger.Logger
	pool       *pgxpool.Pool    // Database pool for health checks
	redis      *pkgredis.Client // Redis client for health checks
	codohue    *codohueStatus   // Recommender state reported by /health; nil when unset
	startTime  time.Time        // Server start time for uptime calculation
}

// NewServer a new HTTP server
func NewServer(cfg *config.Config, log *logger.Logger, pool *pgxpool.Pool, redis *pkgredis.Client, codohue *codohueStatus) *Server {
	s := &Server{
		cfg:       cfg,
		log:       log,
		pool:      pool,
		redis:     redis,
		codohue:   codohue,
		startTime: time.Now(),
	}

	router := chi.NewRouter()

	// Global middlewares
	// Note: middleware.RequestID is intentionally omitted — HTTPMiddleware is the
	// sole source of request ID generation and X-Request-ID header propagation.
	router.Use(middleware.RealIP)
	router.Use(logger.HTTPMiddleware(log))
	router.Use(middleware.Recoverer)
	router.Use(errors.ErrorHandler)

	// CORS middleware
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Server.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID", "Cache-Control", "Last-Event-ID"},
		ExposedHeaders:   []string{"Link", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300, // 5 minutes
	}))

	// Rate limiting - global by IP
	router.Use(httprate.LimitByIP(cfg.Server.RateLimitRequests, cfg.Server.RateLimitWindow))

	// Note: middleware.Timeout is NOT applied globally here.
	// It is applied per route-group in registerRoutes() so that long-lived
	// connections like SSE streams can opt out.

	// Health check endpoint
	router.Get("/health", s.healthCheckHandler)

	// Metrics endpoint
	router.Get("/metrics", s.metricsHandler)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	s.httpServer = httpServer
	s.router = router

	return s
}

// Router returns the chi router for registering routes
func (s *Server) Router() chi.Router {
	return s.router
}

// RequestTimeout returns the configured request timeout middleware.
// Apply this to route groups that should enforce a deadline; omit for SSE/streaming routes.
func (s *Server) RequestTimeout() func(http.Handler) http.Handler {
	return middleware.Timeout(s.cfg.Server.RequestTimeout)
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.log.Info("starting HTTP server",
		"host", s.cfg.Server.Host,
		"port", s.cfg.Server.Port,
	)

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("shutting down HTTP server")

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	s.log.Info("HTTP server stopped")
	return nil
}

// HealthCheckResponse represents the health check response
type HealthCheckResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	// Redis is "up" or "down". Unlike Codohue, down makes the service unhealthy:
	// Redis holds the feed cache, the materialized timeline and the notification
	// Pub/Sub, so an instance that cannot reach it serves a different feed rather
	// than a slower one, and should be taken out of rotation.
	Redis string `json:"redis"`
	// Codohue is "off", "active", or "degraded". Degraded does not make the
	// service unhealthy — the feed falls back to local scoring and the API is
	// fully functional — but it has to be reported somewhere a monitor can see,
	// which is the whole reason this field exists.
	Codohue string `json:"codohue,omitempty"`
	// CodohueReason carries why, and only while degraded.
	CodohueReason string `json:"codohue_reason,omitempty"`
}

// healthCheckHandler handles health check requests
func (s *Server) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	response := HealthCheckResponse{
		Status:   "healthy",
		Database: "up",
		Redis:    "up",
	}
	if s.codohue != nil {
		response.Codohue, response.CodohueReason = s.codohue.get()
	}

	// Both stores are probed before responding, rather than returning on the first
	// failure: an operator reading a 503 wants to know whether one is down or both.
	if s.pool != nil {
		if err := s.pool.Ping(ctx); err != nil {
			s.log.Error("database health check failed", "error", err)
			response.Status = "unhealthy"
			response.Database = "down"
		}
	}
	if s.redis != nil {
		if err := s.redis.Ping(ctx).Err(); err != nil {
			s.log.Error("redis health check failed", "error", err)
			response.Status = "unhealthy"
			response.Redis = "down"
		}
	}

	status := http.StatusOK
	if response.Status != "healthy" {
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.log.Error("failed to encode health check response", "error", err)
	}
}
