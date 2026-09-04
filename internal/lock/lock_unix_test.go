//go:build darwin || linux

package lock_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/someson/azform/internal/lock"
)

func fakeTTY(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "fake-tty-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	})
	return f
}

func withRuntimeDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", dir)
}

func TestAcquireRoundTrip(t *testing.T) {
	tty := fakeTTY(t)
	withRuntimeDir(t, t.TempDir())

	lk, err := lock.Acquire(tty)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if lk == nil {
		t.Fatal("Acquire returned nil lock without error")
	}
	if lk.Path() == "" {
		t.Error("Path() is empty after Acquire")
	}
	if err := lk.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestContentionReturnsErrLocked(t *testing.T) {
	tty := fakeTTY(t)
	withRuntimeDir(t, t.TempDir())

	lk1, err := lock.Acquire(tty)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	t.Cleanup(func() { _ = lk1.Close() })

	_, err = lock.Acquire(tty)
	if !errors.Is(err, lock.ErrLocked) {
		t.Errorf("second Acquire = %v, want ErrLocked", err)
	}
}

func TestDifferentFdsIndependent(t *testing.T) {
	withRuntimeDir(t, t.TempDir())

	lk1, err := lock.Acquire(fakeTTY(t))
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	t.Cleanup(func() { _ = lk1.Close() })

	lk2, err := lock.Acquire(fakeTTY(t))
	if err != nil {
		t.Fatalf("second tty blocked: %v", err)
	}
	t.Cleanup(func() { _ = lk2.Close() })

	if lk1.Path() == lk2.Path() {
		t.Errorf("different fds produced same lock path %q", lk1.Path())
	}
}

func TestLockFileDeletedOnClose(t *testing.T) {
	tty := fakeTTY(t)
	dir := t.TempDir()
	withRuntimeDir(t, dir)

	lk, err := lock.Acquire(tty)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	path := lk.Path()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file %s not created: %v", path, err)
	}
	if err := lk.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("lock file still present after Close: %v", err)
	}
}

func TestLockFileNameAndPerm(t *testing.T) {
	tty := fakeTTY(t)
	dir := t.TempDir()
	withRuntimeDir(t, dir)

	lk, err := lock.Acquire(tty)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = lk.Close() })

	name := filepath.Base(lk.Path())
	if !strings.HasPrefix(name, "azform-") || !strings.HasSuffix(name, ".lock") {
		t.Errorf("lock file name = %q, want azform-*.lock", name)
	}
	info, err := os.Stat(lk.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("lock file perm = %o, want 0600", perm)
	}
}

func TestRuntimeDirUsesXDG(t *testing.T) {
	tty := fakeTTY(t)
	dir := t.TempDir()
	withRuntimeDir(t, dir)

	lk, err := lock.Acquire(tty)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = lk.Close() })

	if filepath.Dir(lk.Path()) != dir {
		t.Errorf("lock path = %q, want dir %q", lk.Path(), dir)
	}
}

func TestRuntimeDirFallback(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	tty := fakeTTY(t)

	lk, err := lock.Acquire(tty)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = lk.Close() })

	if !strings.HasPrefix(lk.Path(), os.TempDir()) {
		t.Errorf("lock path = %q, want prefix %q", lk.Path(), os.TempDir())
	}
}

func TestCloseNilSafe(t *testing.T) {
	var lk *lock.Lock
	if err := lk.Close(); err != nil {
		t.Errorf("nil Close returned %v, want nil", err)
	}
}

func TestAcquireNilTTY(t *testing.T) {
	if _, err := lock.Acquire(nil); err == nil {
		t.Error("Acquire(nil tty) returned nil error, want non-nil")
	}
}
