// Package diagnostics logs parser self-health signals so degradation is
// visible from the field (spec §14.2).
package diagnostics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	healthLogFile = "parse-health.log"
	healthMaxRows = 200
)

// Entry is one parse health record.
type Entry struct {
	Timestamp  time.Time `json:"ts"`
	Command    string    `json:"cmd"`
	Params     int       `json:"params"`
	Unparsed   int       `json:"unparsed"`
	SectionsOK bool      `json:"sections_ok"`
	Suspect    bool      `json:"suspect"`
}

// AppendHealth appends e to <stateDir>/parse-health.log and rotates to the
// last healthMaxRows entries.
func AppendHealth(stateDir string, e Entry, now time.Time) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = now
	}
	if !e.SectionsOK || e.Unparsed > 0 || e.Params == 0 {
		e.Suspect = true
	}
	return writeRotated(stateDir, e)
}

func writeRotated(stateDir string, e Entry) error {
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return fmt.Errorf("diagnostics: create state dir: %w", err)
	}
	path := filepath.Join(stateDir, healthLogFile)

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("diagnostics: marshal: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("diagnostics: open: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("diagnostics: append entry: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("diagnostics: close log: %w", err)
	}
	return rotateIfNeeded(path)
}

// rotateIfNeeded keeps the last healthMaxRows lines (spec §14.2: last 200).
func rotateIfNeeded(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("diagnostics: read log for rotation: %w", err)
	}
	n := 1
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	if n <= healthMaxRows {
		return nil
	}
	lines := splitLines(data)
	keep := lines[len(lines)-healthMaxRows:]
	tmp, err := os.CreateTemp(filepath.Dir(path), ".parse-health-*.tmp")
	if err != nil {
		return fmt.Errorf("diagnostics: create rotation temp file: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	for _, line := range keep {
		if _, err := tmp.Write(line); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("diagnostics: write rotated line: %w", err)
		}
		if _, err := tmp.Write([]byte{'\n'}); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("diagnostics: write rotated newline: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("diagnostics: close rotation temp file: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("diagnostics: rename rotated log: %w", err)
	}
	return nil
}

func splitLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}
