//go:build !darwin && !linux

package lock

import "os"

// Acquire is a no-op on platforms without flock. M9 replaces this stub with
// the real Windows implementation (PSReadLine-aware per-terminal lock).
func Acquire(tty *os.File) (*Lock, error) {
	return nil, nil
}