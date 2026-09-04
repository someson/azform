// Package baseline exposes the pre-baked command metadata embedded into the
// binary via //go:embed (spec §3.4). Lookup is the warm-start fallback used
// by the cache when no on-disk record exists yet.
//
// This package deliberately does NOT import internal/metadata — that would
// create an import cycle (metadata → cache → baseline → metadata). Callers
// unmarshal the returned bytes themselves.
package baseline

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed baseline/*.json
var rawFiles embed.FS

var index []string

func init() {
	entries, err := rawFiles.ReadDir("baseline")
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		index = append(index, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(index)
}

// Raw returns the raw JSON bytes for the embedded record at slug, with ok
// reporting whether the record exists. Callers unmarshal into the type they
// need (CommandRecord vs GroupRecord).
func Raw(slug string) ([]byte, bool) {
	data, err := rawFiles.ReadFile("baseline/" + slug + ".json")
	if err != nil {
		return nil, false
	}
	return data, true
}

// Slugs returns all embedded slugs (sorted).
func Slugs() []string {
	out := make([]string, len(index))
	copy(out, index)
	return out
}

// Size returns the number of embedded records (for diagnostics).
func Size() int { return len(index) }

// String renders a one-line summary for --version / --doctor output.
func String() string {
	return fmt.Sprintf("%d embedded commands", len(index))
}
