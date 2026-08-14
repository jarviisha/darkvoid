package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStorage_Lifecycle(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "uploads")
	store, err := NewLocal(dir, "https://cdn.test/media/")
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	const key = "avatars/user.txt"
	if putErr := store.Put(context.Background(), key, strings.NewReader("avatar"), 6, "text/plain"); putErr != nil {
		t.Fatalf("Put() error = %v", putErr)
	}
	data, err := os.ReadFile(filepath.Join(dir, "avatars", "user.txt"))
	if err != nil || string(data) != "avatar" {
		t.Fatalf("stored data = %q, %v", data, err)
	}
	if got := store.URL(key); got != "https://cdn.test/media/avatars/user.txt" {
		t.Fatalf("URL() = %q", got)
	}
	checker, ok := store.(HealthChecker)
	if !ok || checker.HealthCheck(context.Background()) != nil {
		t.Fatal("local storage health check failed")
	}
	if deleteErr := store.Delete(context.Background(), key); deleteErr != nil {
		t.Fatalf("Delete() error = %v", deleteErr)
	}
	if deleteErr := store.Delete(context.Background(), key); deleteErr != nil {
		t.Fatalf("idempotent Delete() error = %v", deleteErr)
	}
}

func TestNewAndNopStorage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := New(Config{Provider: "local", LocalDir: dir, BaseURL: "https://cdn.test"})
	if err != nil {
		t.Fatalf("New(local) error = %v", err)
	}
	if _, ok := store.(*localStorage); !ok {
		t.Fatalf("store = %T, want localStorage", store)
	}
	nop := NewNop("https://cdn.test/")
	if err := nop.Put(context.Background(), "key", strings.NewReader("data"), 4, "text/plain"); err != nil {
		t.Fatalf("nop Put() error = %v", err)
	}
	if err := nop.Delete(context.Background(), "key"); err != nil {
		t.Fatalf("nop Delete() error = %v", err)
	}
	if nop.URL("key") != "https://cdn.test/key" {
		t.Fatalf("nop URL() = %q", nop.URL("key"))
	}
	if checker, ok := nop.(HealthChecker); !ok || checker.HealthCheck(context.Background()) != nil {
		t.Fatal("nop health check failed")
	}
}
