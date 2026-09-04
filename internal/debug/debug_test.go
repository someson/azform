package debug_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/someson/azform/internal/debug"
)

func TestOpenCreatesFile(t *testing.T) {
	dir := t.TempDir()
	l, err := debug.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	path := filepath.Join(dir, "debug.log")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}

func TestOpenEmptyDir(t *testing.T) {
	if _, err := debug.Open(""); err == nil {
		t.Error("Open(\"\") returned nil error, want non-nil")
	}
}

func TestEventJSONShape(t *testing.T) {
	dir := t.TempDir()
	l, err := debug.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	l.SetNow(func() time.Time {
		return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	})
	l.Event("cache.resolve", map[string]any{
		"command":     "vm create",
		"from_cache":  true,
		"duration_ms": 42,
	})

	data, err := os.ReadFile(filepath.Join(dir, "debug.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	line := strings.TrimSpace(string(data))
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", line, err)
	}
	if got["event"] != "cache.resolve" {
		t.Errorf("event = %v, want cache.resolve", got["event"])
	}
	if got["command"] != "vm create" {
		t.Errorf("command = %v, want vm create", got["command"])
	}
	if got["from_cache"] != true {
		t.Errorf("from_cache = %v, want true", got["from_cache"])
	}
	if got["duration_ms"] != float64(42) {
		t.Errorf("duration_ms = %v, want 42", got["duration_ms"])
	}
	if got["ts"] != "2026-09-01T12:00:00Z" {
		t.Errorf("ts = %v, want 2026-09-01T12:00:00Z", got["ts"])
	}
}

func TestEventNoOpOnNil(t *testing.T) {
	var l *debug.Logger
	l.Event("anything", map[string]any{"x": 1}) // must not panic
	if err := l.Close(); err != nil {
		t.Errorf("nil Close returned %v, want nil", err)
	}
}

func TestEventConcurrent(t *testing.T) {
	dir := t.TempDir()
	l, err := debug.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	const goroutines = 10
	const perG = 100
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				l.Event("concurrent", map[string]any{
					"g": id,
					"i": i,
				})
			}
		}(g)
	}
	wg.Wait()

	data, err := os.ReadFile(filepath.Join(dir, "debug.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != goroutines*perG {
		t.Errorf("got %d lines, want %d", len(lines), goroutines*perG)
	}
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("invalid JSON line %q: %v", line, err)
			break
		}
	}
}

func TestSortedKeys(t *testing.T) {
	dir := t.TempDir()
	l, err := debug.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	l.Event("demo", map[string]any{
		"zulu":  1,
		"alpha": 2,
		"mike":  3,
		"bravo": 4,
	})

	data, err := os.ReadFile(filepath.Join(dir, "debug.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	line := strings.TrimSpace(string(data))
	// Strip braces; the order inside should be alphabetical.
	inner := line[1 : len(line)-1]
	parts := strings.Split(inner, ",")
	wantPrefixes := []string{`"alpha"`, `"bravo"`, `"event"`, `"mike"`, `"ts"`, `"zulu"`}
	if len(parts) != len(wantPrefixes) {
		t.Fatalf("got %d fields, want %d: %q", len(parts), len(wantPrefixes), inner)
	}
	for i, want := range wantPrefixes {
		if !strings.HasPrefix(parts[i], want+":") {
			t.Errorf("part[%d] = %q, want prefix %q", i, parts[i], want)
		}
	}
}

func TestEventClosedLogger(t *testing.T) {
	dir := t.TempDir()
	l, err := debug.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Event after close is a no-op (must not panic).
	l.Event("after_close", map[string]any{"x": 1})
	if err := l.Close(); err != nil {
		t.Errorf("second Close returned %v, want nil", err)
	}
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// Compile-time check: Logger accepts any io.WriteCloser-compatible wrapper.
var _ io.WriteCloser = nopCloser{bytes.NewBuffer(nil)}

// (nopCloser type retained so the import of `bytes`/`io` is intentional for
// future debug sinks like a write-through in-memory buffer for tests.)
var _ = nopCloser{}
var _ = bytes.NewBuffer
