// Package vars loads shell variables passed by the widget and matches them to
// Azure CLI command parameters per azform spec §4.1–4.4.
package vars

import (
	"fmt"
	"os"
	"strings"

	"github.com/someson/azform/internal/state"
)

// Variable is one shell variable passed by the widget.
type Variable struct {
	Name  string
	Value string
}

// ReadFile parses a NUL-separated `NAME=VALUE` file into a slice of Variables.
// Sensitive names (per state.IsSensitiveName) are filtered out. Records
// without '=' are silently skipped so that a partially-written file from the
// widget still produces useful output. Values may contain newlines.
func ReadFile(path string) ([]Variable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	var out []Variable
	for _, rec := range strings.Split(string(data), "\x00") {
		if rec == "" {
			continue
		}
		eq := strings.IndexByte(rec, '=')
		if eq < 0 {
			continue
		}
		name := rec[:eq]
		value := rec[eq+1:]
		if IsSensitiveName(name) {
			continue
		}
		out = append(out, Variable{Name: name, Value: value})
	}
	return out, nil
}

// IsSensitiveName reports whether name matches the sensitive-parameter patterns
// from spec §4.1. Re-exported from state for package symmetry.
func IsSensitiveName(name string) bool {
	return state.IsSensitiveName(name)
}
