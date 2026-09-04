//go:build darwin || linux

// Package term provides the terminal handle used by bubbletea. Opening /dev/tty
// directly lets the TUI paint to the terminal even when stdio is redirected to
// the shell widget's pipes (spec 7.4 / 12.4).
package term

import "os"

// Open opens /dev/tty for reading and writing.
func Open() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}
