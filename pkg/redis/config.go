package redis

import (
	"fmt"
	"time"
)

// Config holds Redis connection configuration.
type Config struct {
	Host         string
	Port         int
	Password     string
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolSize     int
	MinIdleConns int
}

// Addr returns "host:port".
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
