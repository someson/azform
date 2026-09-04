// Package lock enforces "one azform per terminal" (spec §15.2). The lock is
// keyed by the controlling tty's (dev, inode) so multiple terminal windows
// stay independent.
package lock

import (
	"errors"
	"os"
)

// ErrLocked signals that another azform process already holds the per-tty
// lock. Callers MUST exit silently with code 1 — no stderr, no buffer write.
var ErrLocked = errors.New("azform: another instance holds the terminal lock")

// Lock represents one held per-terminal lock. Close releases the flock and
// deletes the underlying lock file.
type Lock struct {
	f    *os.File
	path string
}

// Close releases the flock and removes the lock file. Safe on nil receiver
// (returns nil) and idempotent: subsequent calls on the same Lock return nil.
func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	if rmErr := os.Remove(l.path); err == nil {
		err = rmErr
	}
	return err
}

// Path returns the lock file path. Empty until Acquire succeeds.
func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// runtimeDir returns the directory where lock files live. Honours
// $XDG_RUNTIME_DIR per the XDG Base Directory Specification; falls back to
// the system temp dir when the env var is unset (per spec §15.2).
func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}
	return os.TempDir()
}
