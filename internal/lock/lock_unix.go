//go:build darwin || linux

package lock

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Acquire grabs an exclusive, non-blocking flock on a per-terminal lock file.
// On contention returns ErrLocked; the caller exits silently (spec §15.2:
// "молча выйти с кодом 1, буфер не трогать").
func Acquire(tty *os.File) (*Lock, error) {
	if tty == nil {
		return nil, fmt.Errorf("azform: lock: tty is nil")
	}
	key, err := terminalKey(tty)
	if err != nil {
		return nil, err
	}
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

// terminalKey identifies the terminal session that owns tty.
//
// It deliberately does NOT use the (dev, inode) of the open tty, which spec
// §15.2 suggests: azform opens the path /dev/tty, a single cloning device
// node shared by the whole machine, so fstat returns the same pair in every
// terminal. Keying on it made the lock global and produced exactly the
// failure the spec's anti-pattern §34 warns about — the second terminal
// window stops working. Nor can the fd be resolved back to the underlying
// pty: on darwin fcntl(F_GETPATH) and ttyname(3) both answer "/dev/tty".
//
// TIOCGSID asks the terminal itself for its session id, which is what
// "one azform per terminal" means: unique per terminal, identical for every
// process the shell forks inside it, and unaffected by which process asks.
func terminalKey(tty *os.File) (string, error) {
	if sid, err := unix.IoctlGetInt(int(tty.Fd()), tiocgsid); err == nil && sid > 0 {
		return fmt.Sprintf("sid-%d", sid), nil
	}
	// The fd has no session — it is not a terminal, or the terminal has no
	// controlling session. Fall back to this process's own session, which
	// still separates one terminal's processes from another's.
	sid, err := unix.Getsid(0)
	if err != nil {
		return "", fmt.Errorf("azform: lock: terminal session id: %w", err)
	}
	return fmt.Sprintf("sid-%d", sid), nil
}
