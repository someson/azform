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
