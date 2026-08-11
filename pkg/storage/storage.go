package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Config mirrors config.StorageConfig to avoid import cycle.
type Config struct {
	Provider string
	BaseURL  string
	LocalDir string
}

// New creates a Storage from config, selecting the appropriate provider.
// Only "local" is supported. Unknown or unimplemented providers fail closed so
// an upload can never be reported successful without persisting its bytes.
func New(cfg Config) (Storage, error) {
	switch cfg.Provider {
	case "local":
		return NewLocal(cfg.LocalDir, cfg.BaseURL)
	default:
		return nil, fmt.Errorf("storage: unsupported provider %q", cfg.Provider)
	}
}

// Storage defines the interface for file storage operations.
// Implementations are provider-agnostic (local disk, S3, GCS, MinIO, etc.)
type Storage interface {
	// Put uploads a file and stores it at the given key.
	// key is a relative path like "avatars/abc123.jpg"
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error

	// Delete removes the file at the given key.
	// Returns nil if the file does not exist (idempotent).
	Delete(ctx context.Context, key string) error

	// URL builds the public-accessible URL for a given key.
	// This is called at response time, never stored in the database.
	URL(key string) string
}

// nopStorage is a test double that performs no I/O.
// Put and Delete succeed silently; URL builds a URL from baseURL + key.
type nopStorage struct {
	baseURL string
}

// NewNop returns a Storage test double that performs no real I/O. Production
// configuration must go through New, which never selects this implementation.
func NewNop(baseURL string) Storage {
	return &nopStorage{baseURL: strings.TrimRight(baseURL, "/")}
}

func (n *nopStorage) Put(_ context.Context, _ string, _ io.Reader, _ int64, _ string) error {
	return nil
}

func (n *nopStorage) Delete(_ context.Context, _ string) error {
	return nil
}

func (n *nopStorage) URL(key string) string {
	return fmt.Sprintf("%s/%s", n.baseURL, key)
}
