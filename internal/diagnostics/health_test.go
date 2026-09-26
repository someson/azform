package diagnostics_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/someson/azform/internal/diagnostics"
)

func TestAppendHealthCreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := diagnostics.AppendHealth(dir, diagnostics.Entry{
		Command: "vm create",
		Params:  5, Unparsed: 0, SectionsOK: true,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "parse-health.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("log file is empty")
	}
}

func TestAppendHealthRotation(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 250; i++ {
		_ = diagnostics.AppendHealth(dir, diagnostics.Entry{
			Command: "cmd",
			Params:  i, Unparsed: 0, SectionsOK: true,
		}, now.Add(time.Duration(i)*time.Second))
	}
	path := filepath.Join(dir, "parse-health.log")
	data, _ := os.ReadFile(path)
	lines := splitLines(data)
	if len(lines) > 200 {
		t.Errorf("log has %d entries, want <= 200", len(lines))
	}
}

func TestAppendHealthParsesJSON(t *testing.T) {
	dir := t.TempDir()
	if err := diagnostics.AppendHealth(dir, diagnostics.Entry{
		Command: "vm create",
		Params:  12, Unparsed: 0, SectionsOK: true,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "parse-health.log")
	data, _ := os.ReadFile(path)
	first := splitLines(data)[0]
	var e diagnostics.Entry
	if err := json.Unmarshal(first, &e); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, first)
	}
	if e.Command != "vm create" || e.Params != 12 {
		t.Errorf("got %+v", e)
	}
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

// A full log (exactly 200 entries) is left alone: rotation rewrites the
// file only when an append takes it past the limit.
func TestAppendHealthNoRewriteAtLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 199; i++ {
		_ = diagnostics.AppendHealth(dir, diagnostics.Entry{Command: "cmd", Params: i, SectionsOK: true}, now)
	}
	path := filepath.Join(dir, "parse-health.log")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// The 200th entry fits: rotation (temp file + rename) must not run.
	_ = diagnostics.AppendHealth(dir, diagnostics.Entry{Command: "cmd", Params: 199, SectionsOK: true}, now)
	at, _ := os.Stat(path)
	if !os.SameFile(before, at) {
		t.Errorf("log rewritten at exactly 200 entries")
	}
	before = at
	data, _ := os.ReadFile(path)
	if n := len(splitLines(data)); n != 200 {
		t.Fatalf("log has %d entries, want 200", n)
	}
	_ = diagnostics.AppendHealth(dir, diagnostics.Entry{Command: "cmd", Params: 1, SectionsOK: true}, now)
	after, _ := os.Stat(path)
	if os.SameFile(before, after) {
		t.Errorf("201st entry should rotate the log")
	}
	data, _ = os.ReadFile(path)
	if n := len(splitLines(data)); n != 200 {
		t.Errorf("after rotation: %d entries, want 200", n)
	}
}
