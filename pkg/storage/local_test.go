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

// The probe has to live in the upload directory to prove that directory is
// writable, so its name is what keeps it out of reach while it exists: the
// static route answers 404 for any dot-prefixed segment
// (middleware.HiddenFileGuard). Changing the prefix to something the guard does
// not cover makes the probe fetchable for as long as it is on disk.
func TestLocalHealthCheck_ProbeIsHiddenAndRemoved(t *testing.T) {
	t.Parallel()
	if !strings.HasPrefix(healthProbePrefix, ".") {
		t.Fatalf("health probe prefix %q is not dot-prefixed and would be served under /static/", healthProbePrefix)
	}

	dir := t.TempDir()
	store, err := NewLocal(dir, "https://cdn.test/static")
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	if err = store.(HealthChecker).HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("upload directory holds %d entries after the probe, want 0", len(entries))
	}
}

// TestOwnershipMessage_MismatchNamesTheOwnerAndTheRemedy covers the upgrade
// hazard this message exists for, using literals because the filesystem cannot
// produce it here: a directory owned by root can only be created by root.
//
// A Docker named volume takes its ownership from the image that first populated
// it, so an uploads volume created before this image ran unprivileged is owned
// by root, the boot is refused, and "permission denied" points at the path when
// the answer is the owner.
func TestOwnershipMessage_MismatchNamesTheOwnerAndTheRemedy(t *testing.T) {
	msg := ownershipMessage("/app/uploads", 0, 0, 0o755, 100, 101)

	for _, want := range []string{
		"owned by uid 0 gid 0", // who holds it
		"uid 100 gid 101",      // who this process is
		"chown -R 100:101",     // what someone outside has to run
		"outside the container",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
}

// TestLocalHealthCheck_OwnedButUnwritableBlamesTheMode separates the two ways
// this probe fails. When the directory already belongs to this process, the
// owner is not the story and chowning it to the uid it already has is advice
// that cannot work — the mode is what denies the write.
func TestLocalHealthCheck_OwnedButUnwritableBlamesTheMode(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the permission bits this test depends on")
	}

	dir := t.TempDir() // owned by this process
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	store, err := NewLocal(dir, "http://localhost:8080/static")
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	err = store.(HealthChecker).HealthCheck(context.Background())
	if err == nil {
		t.Fatal("HealthCheck on a read-only directory returned nil")
	}

	msg := err.Error()
	if strings.Contains(msg, "chown") {
		t.Errorf("error %q recommends chown, but the directory already belongs to this process", msg)
	}
	if !strings.Contains(msg, "mode") {
		t.Errorf("error %q does not name the mode as the cause", msg)
	}
}
