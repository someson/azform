// Command gen-baseline walks testdata/help/, parses each fixture via
// metadata.Parse, and writes the resulting CommandRecord (or GroupRecord)
// into internal/metadata/baseline/baseline/<slug>.json. The output is
// embedded into the binary by //go:embed in the baseline package.
//
// Run from the repo root:
//
//	go run ./cmd/gen-baseline
//
// Output is regenerated when fixtures change or after a parser change.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/someson/azform/internal/metadata"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-baseline:", err)
		os.Exit(1)
	}
}

func run() error {
	src := "testdata/help"
	dst := filepath.Join("internal", "metadata", "baseline", "baseline")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	files, err := filepath.Glob(filepath.Join(src, "*.txt"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no fixtures in %s", src)
	}

	now := nowFunc()
	count := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "skip", path, err)
			continue
		}
		doc, err := metadata.Parse(string(data))
		if err != nil {
			fmt.Fprintln(os.Stderr, "skip", path, "parse:", err)
			continue
		}
		slug := fileSlug(path)
		out := filepath.Join(dst, slug+".json")
		switch doc.Kind {
		case metadata.DocumentKindCommand:
			rec := metadata.CommandRecord{
				SchemaVersion: metadata.SchemaVersion,
				Command:       doc.Command.Command,
				Summary:       doc.Command.Summary,
				AZVersion:     "embedded",
				AzformVersion: os.Getenv("AZFORM_VERSION"),
				Source:        metadata.SourceHelpParser,
				GeneratedAt:   now,
				ParseHealth:   doc.ParseHealth,
				Parameters:    doc.Command.Parameters,
			}
			b, err := json.MarshalIndent(rec, "", "  ")
			if err != nil {
				return err
			}
			if err := atomicWrite(out, b); err != nil {
				return err
			}
			count++
		case metadata.DocumentKindGroup:
			rec := metadata.GroupRecord{
				SchemaVersion: metadata.SchemaVersion,
				Group:         doc.Group.Group,
				Summary:       doc.Group.Summary,
				AZVersion:     "embedded",
				AzformVersion: os.Getenv("AZFORM_VERSION"),
				Source:        metadata.SourceHelpParser,
				GeneratedAt:   now,
				ParseHealth:   doc.ParseHealth,
				Subgroups:     doc.Group.Subgroups,
				Commands:      doc.Group.Commands,
			}
			b, err := json.MarshalIndent(rec, "", "  ")
			if err != nil {
				return err
			}
			if err := atomicWrite(out, b); err != nil {
				return err
			}
			count++
		}
	}
	fmt.Printf("wrote %d baseline records to %s\n", count, dst)
	return nil
}

// fileSlug derives the cache slug from the help filename (without .txt).
// "storage-account-create.txt" → "storage_account_create".
func fileSlug(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".txt")
	var b strings.Builder
	for _, r := range base {
		if r == '-' {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".baseline-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

var nowFunc = func() time.Time { return time.Now().UTC() }
