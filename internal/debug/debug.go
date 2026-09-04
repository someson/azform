// Package debug emits structured one-line JSON events to the azform debug
// log (spec §15.3). The log lives at <stateDir>/debug.log; nil Logger is a
// no-op so production code can call Event without checking for nil.
package debug

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Logger writes JSONL events to an append-only file.
type Logger struct {
	mu  sync.Mutex
	w   io.WriteCloser
	now func() time.Time
}

// Open returns a Logger writing to <stateDir>/debug.log. stateDir is created
// (0750) if missing. File is opened with O_APPEND|O_CREATE, mode 0600.
func Open(stateDir string) (*Logger, error) {
	if stateDir == "" {
		return nil, os.ErrInvalid
	}
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(stateDir, "debug.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{w: f, now: func() time.Time { return time.Now().UTC() }}, nil
}

// SetNow overrides the timestamp source (tests only).
func (l *Logger) SetNow(now func() time.Time) {
	l.mu.Lock()
	l.now = now
	l.mu.Unlock()
}

// Event appends one JSONL record: {"ts": ..., "event": name, ...fields}.
// Keys are emitted in alphabetical order for stable diffs. No-op on nil
// receiver.
func (l *Logger) Event(name string, fields map[string]any) {
	if l == nil || l.w == nil {
		return
	}
	e := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		e[k] = v
	}
	l.mu.Lock()
	now := l.now
	l.mu.Unlock()
	e["ts"] = now().Format(time.RFC3339Nano)
	e["event"] = name

	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf []byte
	buf = append(buf, '{')
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, _ := json.Marshal(k)
		buf = append(buf, kb...)
		buf = append(buf, ':')
		vb, err := json.Marshal(e[k])
		if err != nil {
			vb = []byte(`null`)
		}
		buf = append(buf, vb...)
	}
	buf = append(buf, '}', '\n')

	l.mu.Lock()
	_, _ = l.w.Write(buf)
	l.mu.Unlock()
}

// Close flushes and closes the underlying file. Safe on nil; idempotent.
func (l *Logger) Close() error {
	if l == nil || l.w == nil {
		return nil
	}
	err := l.w.Close()
	l.w = nil
	return err
}
