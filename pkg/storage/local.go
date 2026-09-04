package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// healthProbePrefix names the file HealthCheck writes into the upload
// directory. The leading dot is load-bearing: the static route refuses
// dot-prefixed segments, which is what keeps the probe unreachable for the
// moment it exists inside a directory that is otherwise served verbatim.
const healthProbePrefix = ".storage-health-"

// localStorage stores files on the local filesystem.
// Files are served via a static file server mounted at BaseURL.
type localStorage struct {
	dir     string // absolute path to upload directory, e.g. "/app/uploads"
	baseURL string // public base URL, e.g. "http://localhost:8080/static"
}

// NewLocal returns a Storage backed by the local filesystem.
//
// dir is the directory where files will be written.
// baseURL is the public-facing base URL used to build download URLs.
//
// Example:
//
//	NewLocal("./uploads", "http://localhost:8080/static")
//	// key "avatars/abc.jpg" → URL "http://localhost:8080/static/avatars/abc.jpg"
func NewLocal(dir, baseURL string) (Storage, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("storage/local: resolve dir %q: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("storage/local: create dir %q: %w", abs, err)
	}
	return &localStorage{
		dir:     abs,
		baseURL: strings.TrimRight(baseURL, "/"),
	}, nil
}

// Put writes r to dir/key, creating parent directories as needed.
// size and contentType are accepted for interface compatibility but not used by the local provider.
func (l *localStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	dest := filepath.Join(l.dir, filepath.FromSlash(key))

	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("storage/local: mkdir for key %q: %w", key, err)
	}

	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("storage/local: open %q: %w", dest, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("storage/local: write %q: %w", dest, err)
	}
	return nil
}

// Delete removes the file at dir/key.
// Returns nil if the file does not exist (idempotent).
func (l *localStorage) Delete(_ context.Context, key string) error {
	dest := filepath.Join(l.dir, filepath.FromSlash(key))
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage/local: delete %q: %w", dest, err)
	}
	return nil
}

// URL builds the public URL for a given key.
func (l *localStorage) URL(key string) string {
	return fmt.Sprintf("%s/%s", l.baseURL, key)
}

// HealthCheck verifies that the shared process path is still writable.
//
// The probe is written into the upload directory itself, because a different
// directory would prove a different mount is writable. It is removed as soon as
// it is created, and its dot-prefixed name keeps it unreachable through the
// static route for the moment it exists: middleware.HiddenFileGuard answers 404
// for any path segment beginning with a dot.
func (l *localStorage) HealthCheck(_ context.Context) error {
	f, err := os.CreateTemp(l.dir, healthProbePrefix+"*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("storage/local: %s: %w", l.ownershipHint(), err)
		}
		return fmt.Errorf("storage/local: create health probe in %q: %w", l.dir, err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("storage/local: close health probe %q: %w", name, err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("storage/local: remove health probe %q: %w", name, err)
	}
	return nil
}

// ownershipHint explains a permission failure in terms of who owns the
// directory and who this process is.
func (l *localStorage) ownershipHint() string {
	uid, gid := os.Geteuid(), os.Getegid()

	info, err := os.Stat(l.dir)
	if err != nil {
		return fmt.Sprintf("cannot write to %q as uid %d gid %d", l.dir, uid, gid)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Sprintf("cannot write to %q as uid %d gid %d", l.dir, uid, gid)
	}

	return ownershipMessage(l.dir, stat.Uid, stat.Gid, info.Mode().Perm(), uid, gid)
}

// ownershipMessage is separated from the stat so both of its branches can be
// exercised. The mismatch branch is the one that matters and the one a test
// cannot reach through the filesystem: reproducing it needs a directory owned by
// another user, which the unprivileged process running the tests cannot create.
//
// A Docker named volume takes its ownership from the image that first populated
// it, so an uploads volume created before this image ran unprivileged is owned
// by root and unwritable here. "permission denied" alone sends the reader
// looking at the path; the owner is the answer. The remedy is spelled out
// because this process cannot perform it — chowning the volume is exactly what
// an unprivileged container cannot do to itself.
func ownershipMessage(dir string, owner, group uint32, mode fs.FileMode, uid, gid int) string {
	if int(owner) == uid {
		// Ownership is already right, so chowning it to the uid it has would
		// change nothing. The mode is what refuses the write.
		return fmt.Sprintf(
			"cannot write to %q: it is owned by this process (uid %d) but its mode is %v",
			dir, uid, mode,
		)
	}

	return fmt.Sprintf(
		"cannot write to %q: it is owned by uid %d gid %d and this process runs as uid %d gid %d; "+
			"a volume created by an image that ran as root needs `chown -R %d:%d` from outside the container",
		dir, owner, group, uid, gid, uid, gid,
	)
}
