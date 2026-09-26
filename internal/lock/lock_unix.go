//go:build darwin || linux

package lock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

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
	dir, err := runtimeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "azform-"+key+".lock")

	// Close unlinks the file, so a competitor can open the old inode just
	// before the unlink and flock it just after — while a third process
	// creates and locks a fresh file at the same path. Re-checking that
	// the locked fd is still the file at path closes that window; a few
	// retries cover repeated losses of the race.
	for attempt := 0; attempt < 5; attempt++ {
		// O_NOFOLLOW: never let a planted symlink redirect the open.
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW, 0o600)
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
		if sameFile(f, path) {
			return &Lock{f: f, path: path}, nil
		}
		_ = f.Close()
	}
	return nil, ErrLocked
}

// sameFile reports whether the open file f is still the file at path.
func sameFile(f *os.File, path string) bool {
	held, err := f.Stat()
	if err != nil {
		return false
	}
	cur, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return os.SameFile(held, cur)
}

// runtimeDir returns the directory where lock files live: $XDG_RUNTIME_DIR
// per the XDG Base Directory Specification (per-user and private by
// definition), else a per-user directory under the system temp dir (spec
// §15.2). The fallback is not the temp dir itself: /tmp is shared, so any
// other user could pre-create azform-sid-<n>.lock (session ids are easy to
// guess) and make every Acquire fail, or plant a symlink there.
func runtimeDir() (string, error) {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d, nil
	}
	uid := os.Getuid()
	dir := filepath.Join(os.TempDir(), "azform-"+strconv.Itoa(uid))
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("azform: lock: create %s: %w", dir, err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("azform: lock: stat %s: %w", dir, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(st.Uid) != uid || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("azform: lock: %s is not a private directory owned by uid %d", dir, uid)
	}
	return dir, nil
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
