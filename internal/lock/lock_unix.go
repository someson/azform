//go:build darwin || linux

package lock

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Acquire grabs an exclusive, non-blocking flock on a per-tty lock file. The
// key is (dev, inode) of the open tty file — stable per terminal session,
// independent across terminal windows. On contention returns ErrLocked; the
// caller exits silently (spec §15.2: "молча выйти с кодом 1, буфер не трогать").
func Acquire(tty *os.File) (*Lock, error) {
	if tty == nil {
		return nil, fmt.Errorf("azform: lock: tty is nil")
	}
	var st unix.Stat_t
	if err := unix.Fstat(int(tty.Fd()), &st); err != nil {
		return nil, fmt.Errorf("azform: lock: fstat tty: %w", err)
	}
	// uint64 conversions are needed on BSDs where Dev/Ino are int32/uint32.
	key := fmt.Sprintf("%x-%x", uint64(st.Dev), uint64(st.Ino)) //nolint:unconvert // cross-platform
	path := runtimeDir() + "/azform-" + key + ".lock"

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("azform: lock: open %s: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("azform: lock: flock %s: %w", path, err)
	}
	return &Lock{f: f, path: path}, nil
}
