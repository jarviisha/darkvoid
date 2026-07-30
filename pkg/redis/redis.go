package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps *redis.Client and exposes only what the app needs.
// Use the embedded Client for full access when needed.
type Client struct {
	*redis.Client
}

// New creates a new Redis client, pings the server, and returns it.
// Use this for a Redis the app cannot run correctly without.
func New(ctx context.Context, cfg *Config) (*Client, error) {
	client := NewLazy(cfg)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return client, nil
}

// NewLazy creates a client without verifying connectivity. go-redis dials on
// first use and reconnects on its own, so a server that is down right now starts
// working once it comes back.
//
// Use this for an optional dependency, where discarding the client on a failed
// boot probe would disable the feature for the whole life of the process rather
// than for the length of the outage. Callers that want to report reachability
// should call HealthCheck and log the result instead of acting on it.
func NewLazy(cfg *Config) *Client {
	return &Client{redis.NewClient(&redis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
	})}
}

// Close closes the underlying connection pool.
func (c *Client) Close() error {
	return c.Client.Close()
}

// HealthCheck pings Redis and returns an error if unhealthy.
func HealthCheck(ctx context.Context, c *Client) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := c.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis health check failed: %w", err)
	}
	return nil
}
