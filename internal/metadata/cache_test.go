package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/someson/azform/internal/debug"
)

type fixtureRunner struct {
	helpByPath map[string]string
	version    string
	calls      int
	lastPath   []string
	err        error
}

func (r *fixtureRunner) Resolve(_ context.Context, commandPath []string) (*Document, string, error) {
	r.calls++
	r.lastPath = append([]string(nil), commandPath...)
	if r.err != nil {
		return nil, "", r.err
	}
	fixture, ok := r.helpByPath[strings.Join(commandPath, " ")]
	if !ok {
		return nil, "", errors.New("unexpected command path")
	}
	doc, err := Parse(fixture)
	if err != nil {
		return nil, "", err
	}
	return doc, r.version, nil
}

func testEnvironment(modTime time.Time) Environment {
	return Environment{
		AZPath:            "/fake/az",
		InstallPath:       "/fake/az-install",
		InstallModTime:    modTime.UTC(),
		ExtensionsPath:    "/fake/extensions",
		ExtensionsModTime: modTime.Add(time.Minute).UTC(),
	}
}

func newTestCache(t *testing.T, runner *fixtureRunner, env Environment, version string) *Cache {
	t.Helper()
	cache := NewCache(t.TempDir(), version, runner)
	cache.DetectEnvironment = func() (Environment, error) { return env, nil }
	cache.Now = func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }
	cache.DisableBaseline = true
	cache.DisableHealthLog = true
	return cache
}

func TestCacheLazyWriteAndWarmHit(t *testing.T) {
	runner := &fixtureRunner{
		helpByPath: map[string]string{"group create": readFixture(t, "group-create.txt")},
		version:    "2.89.1",
	}
	env := testEnvironment(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	cache := newTestCache(t, runner, env, "0.1.0")

	first, err := cache.Resolve(context.Background(), " group   create ")
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if first.FromCache || first.Stale || first.Command == nil {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls after miss: got %d", runner.calls)
	}
	if strings.Join(runner.lastPath, " ") != "group create" {
		t.Fatalf("runner path: %v", runner.lastPath)
	}
	if first.Command.AZVersion != "2.89.1" || first.Command.AzformVersion != "0.1.0" {
		t.Fatalf("record versions: %+v", first.Command)
	}
	if _, err := os.Stat(filepath.Join(cache.Dir, "commands", "group_create.json")); err != nil {
		t.Fatalf("command cache was not written: %v", err)
	}
	if entries, err := os.ReadDir(cache.Dir); err != nil || len(entries) != 1 || entries[0].Name() != "commands" {
		// Only the per-command record is written. Staleness is derived from the
		// record timestamp and current mtimes; there is no global state file.
		t.Fatalf("unexpected cache root entries: entries=%v err=%v", entries, err)
	}

	second, err := cache.Resolve(context.Background(), "group create")
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if !second.FromCache || second.Stale || second.Command == nil {
		t.Fatalf("unexpected warm result: %+v", second)
	}
	if runner.calls != 1 {
		t.Fatalf("warm hit spawned az: got %d calls", runner.calls)
	}
}

func TestCacheMTimeChangeReturnsStaleAndRefreshUpdates(t *testing.T) {
	runner := &fixtureRunner{
		helpByPath: map[string]string{"group create": readFixture(t, "group-create.txt")},
		version:    "2.89.1",
	}
	oldEnv := testEnvironment(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	cache := newTestCache(t, runner, oldEnv, "0.1.0")
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cache.Now = func() time.Time { return now }
	if _, err := cache.Resolve(context.Background(), "group create"); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	newEnv := oldEnv
	newEnv.InstallModTime = now.Add(time.Hour)
	cache.DetectEnvironment = func() (Environment, error) { return newEnv, nil }

	stale, err := cache.Resolve(context.Background(), "group create")
	if err != nil {
		t.Fatalf("stale Resolve: %v", err)
	}
	if !stale.FromCache || !stale.Stale || stale.Refresh == nil {
		t.Fatalf("expected immediate stale cache result with refresh hook: %+v", stale)
	}
	if runner.calls != 1 {
		t.Fatalf("stale hit refreshed synchronously: got %d calls", runner.calls)
	}

	now = newEnv.InstallModTime.Add(time.Hour)
	if err := stale.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if runner.calls != 2 {
		t.Fatalf("refresh did not rerun az: got %d calls", runner.calls)
	}
	fresh, err := cache.Resolve(context.Background(), "group create")
	if err != nil {
		t.Fatalf("Resolve after refresh: %v", err)
	}
	if fresh.Stale || !fresh.FromCache {
		t.Fatalf("entry still stale after refresh: %+v", fresh)
	}
}

func TestCacheMTimeInvalidationIsPerCommand(t *testing.T) {
	runner := &fixtureRunner{
		helpByPath: map[string]string{
			"group create": readFixture(t, "group-create.txt"),
			"group list":   readFixture(t, "group-list.txt"),
		},
		version: "2.89.1",
	}
	oldEnv := testEnvironment(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	cache := newTestCache(t, runner, oldEnv, "0.1.0")
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cache.Now = func() time.Time { return now }
	if _, err := cache.Resolve(context.Background(), "group create"); err != nil {
		t.Fatalf("prime group create: %v", err)
	}
	if _, err := cache.Resolve(context.Background(), "group list"); err != nil {
		t.Fatalf("prime group list: %v", err)
	}

	newEnv := oldEnv
	newEnv.InstallModTime = now.Add(time.Hour)
	cache.DetectEnvironment = func() (Environment, error) { return newEnv, nil }

	createStale, err := cache.Resolve(context.Background(), "group create")
	if err != nil || !createStale.Stale {
		t.Fatalf("group create should be stale: result=%+v err=%v", createStale, err)
	}
	now = newEnv.InstallModTime.Add(time.Hour)
	if err := createStale.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh group create: %v", err)
	}

	listResult, err := cache.Resolve(context.Background(), "group list")
	if err != nil {
		t.Fatalf("resolve group list: %v", err)
	}
	if !listResult.Stale {
		t.Fatalf("unrelated cached command became fresh without revalidation: %+v", listResult)
	}
}

func TestCacheAzformVersionChangeMarksStale(t *testing.T) {
	runner := &fixtureRunner{
		helpByPath: map[string]string{"group create": readFixture(t, "group-create.txt")},
		version:    "2.89.1",
	}
	env := testEnvironment(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	oldCache := newTestCache(t, runner, env, "0.1.0")
	if _, err := oldCache.Resolve(context.Background(), "group create"); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	newCache := NewCache(oldCache.Dir, "0.2.0", runner)
	newCache.DetectEnvironment = func() (Environment, error) { return env, nil }
	result, err := newCache.Resolve(context.Background(), "group create")
	if err != nil {
		t.Fatalf("Resolve with upgraded azform: %v", err)
	}
	if !result.Stale || !result.FromCache || result.Refresh == nil {
		t.Fatalf("azform version mismatch was not stale: %+v", result)
	}
	if runner.calls != 1 {
		t.Fatalf("version mismatch caused synchronous refresh: got %d calls", runner.calls)
	}
}

func TestCacheStoresGroupNavigationSeparately(t *testing.T) {
	runner := &fixtureRunner{
		helpByPath: map[string]string{"storage account": readFixture(t, "storage-account.txt")},
		version:    "2.89.1",
	}
	env := testEnvironment(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	cache := newTestCache(t, runner, env, "0.1.0")

	result, err := cache.Resolve(context.Background(), "storage account")
	if err != nil {
		t.Fatalf("Resolve group: %v", err)
	}
	if result.Kind != DocumentKindGroup || result.Group == nil {
		t.Fatalf("expected group result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(cache.Dir, "groups", "storage_account.json")); err != nil {
		t.Fatalf("group cache was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache.Dir, "commands", "storage_account.json")); !os.IsNotExist(err) {
		t.Fatalf("group was unexpectedly written as command: %v", err)
	}
}

func TestCacheCorruptEntryIsReplaced(t *testing.T) {
	runner := &fixtureRunner{
		helpByPath: map[string]string{"group create": readFixture(t, "group-create.txt")},
		version:    "2.89.1",
	}
	env := testEnvironment(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	cache := newTestCache(t, runner, env, "0.1.0")
	path := filepath.Join(cache.Dir, "commands", "group_create.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := cache.Resolve(context.Background(), "group create")
	if err != nil {
		t.Fatalf("Resolve corrupt cache: %v", err)
	}
	if result.FromCache || result.Command == nil || result.Command.Command != "group create" {
		t.Fatalf("corrupt cache was not replaced by live parse: %+v", result)
	}
}

func TestSlug(t *testing.T) {
	if got := Slug(" storage   account create "); got != "storage_account_create" {
		t.Fatalf("Slug: got %q", got)
	}
}

func TestParseAZVersion(t *testing.T) {
	version, err := parseAZVersion([]byte(`{"azure-cli":"2.89.1","extensions":{}}`))
	if err != nil || version != "2.89.1" {
		t.Fatalf("parseAZVersion: version=%q err=%v", version, err)
	}
	if _, err := parseAZVersion([]byte(`{"extensions":{}}`)); err == nil {
		t.Fatalf("missing azure-cli version was accepted")
	}
}

func TestAzureCLIInstallRoot(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "homebrew cellar", path: filepath.Join(sep, "opt", "homebrew", "Cellar", "azure-cli", "2.89.1", "bin", "az"), want: filepath.Join(sep, "opt", "homebrew", "Cellar", "azure-cli", "2.89.1")},
		{name: "deb layout", path: filepath.Join(sep, "opt", "az", "bin", "az"), want: filepath.Join(sep, "opt", "az")},
		{name: "rhel layout", path: filepath.Join(sep, "lib64", "az", "bin", "az"), want: filepath.Join(sep, "lib64", "az")},
		{name: "generic bin", path: filepath.Join(sep, "tmp", "cli", "bin", "az"), want: filepath.Join(sep, "tmp", "cli")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := azureCLIInstallRoot(tt.path); got != tt.want {
				t.Fatalf("azureCLIInstallRoot(%q): got %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCacheRecordSchemaRoundTrip(t *testing.T) {
	doc := parseFixture(t, "group-create.txt")
	record := CommandRecord{
		SchemaVersion: SchemaVersion,
		Command:       doc.Command.Command,
		Summary:       doc.Command.Summary,
		AZVersion:     "2.89.1",
		AzformVersion: "0.1.0",
		Source:        SourceHelpParser,
		GeneratedAt:   time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
		ParseHealth:   doc.ParseHealth,
		Parameters:    doc.Command.Parameters,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	for _, key := range []string{"schema_version", "command", "summary", "az_version", "azform_version", "source", "generated_at", "parse_health", "parameters"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("cache record missing key %q: %s", key, data)
		}
	}
}

func TestCacheResolveEmitsDebugEvents(t *testing.T) {
	runner := &fixtureRunner{
		helpByPath: map[string]string{"group create": readFixture(t, "group-create.txt")},
		version:    "2.89.1",
	}
	env := testEnvironment(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	cache := newTestCache(t, runner, env, "0.1.0")
	debugDir := t.TempDir()
	dbg, err := debug.Open(debugDir)
	if err != nil {
		t.Fatalf("debug.Open: %v", err)
	}
	t.Cleanup(func() { _ = dbg.Close() })
	cache.Debug = dbg

	if _, err := cache.Resolve(context.Background(), "group create"); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if _, err := cache.Resolve(context.Background(), "group create"); err != nil {
		t.Fatalf("second Resolve: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(debugDir, "debug.log"))
	if err != nil {
		t.Fatalf("ReadFile debug.log: %v", err)
	}
	lines := nonEmptyLines(string(data))
	if len(lines) < 3 {
		t.Fatalf("expected >=3 debug lines, got %d: %q", len(lines), lines)
	}

	wantEvents := []string{"az_env", "az_command", "az_command.done", "cache.resolve"}
	seen := map[string]int{}
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid JSON %q: %v", line, err)
		}
		if name, ok := m["event"].(string); ok {
			seen[name]++
		}
	}
	for _, want := range wantEvents {
		if seen[want] == 0 {
			t.Errorf("event %q not logged; seen=%v", want, seen)
		}
	}
	if seen["cache.resolve"] != 2 {
		t.Errorf("cache.resolve count = %d, want 2", seen["cache.resolve"])
	}
}

func TestCacheStatsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	s := CacheStats(dir)
	if s.Files != 0 || s.Bytes != 0 {
		t.Errorf("empty stats = %+v, want zeros", s)
	}
	if !s.OldestEntry.IsZero() || !s.NewestEntry.IsZero() {
		t.Errorf("empty times should be zero, got oldest=%v newest=%v", s.OldestEntry, s.NewestEntry)
	}
}

func TestCacheStatsPopulated(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "commands"))
	mustMkdir(t, filepath.Join(dir, "groups"))

	old := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	mid := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	writeJSONWithMtime(t, filepath.Join(dir, "commands", "a.json"), `{}`, old)
	writeJSONWithMtime(t, filepath.Join(dir, "commands", "b.json"), `{}`, mid)
	writeJSONWithMtime(t, filepath.Join(dir, "groups", "g.json"), `{}`, newest)

	s := CacheStats(dir)
	if s.Files != 3 {
		t.Errorf("Files = %d, want 3", s.Files)
	}
	if s.Bytes != 6 {
		t.Errorf("Bytes = %d, want 6", s.Bytes)
	}
	if !s.OldestEntry.Equal(old) {
		t.Errorf("OldestEntry = %v, want %v", s.OldestEntry, old)
	}
	if !s.NewestEntry.Equal(newest) {
		t.Errorf("NewestEntry = %v, want %v", s.NewestEntry, newest)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeJSONWithMtime(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
