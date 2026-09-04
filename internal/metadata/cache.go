package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/someson/azform/internal/debug"
	"github.com/someson/azform/internal/diagnostics"
	"github.com/someson/azform/internal/metadata/baseline"
)

// Environment is the cheap filesystem fingerprint used for invalidation.
// Getting it never starts az.
type Environment struct {
	AZPath            string    `json:"az_path"`
	InstallPath       string    `json:"install_path"`
	InstallModTime    time.Time `json:"install_mtime"`
	ExtensionsPath    string    `json:"extensions_path,omitempty"`
	ExtensionsModTime time.Time `json:"extensions_mtime,omitempty"`
}

// Result is a cache lookup. A stale result is deliberately returned without
// waiting; the caller may run Refresh in a background goroutine.
type Result struct {
	Kind      DocumentKind
	Stale     bool
	FromCache bool
	Command   *CommandRecord
	Group     *GroupRecord
	Refresh   func(context.Context) error
}

// Cache is the lazy per-command metadata cache from spec 3.3.
type Cache struct {
	Dir               string
	AzformVersion     string
	Runner            Runner
	DetectEnvironment func() (Environment, error)
	Now               func() time.Time
	// DisableBaseline, when true, makes Resolve ignore the embedded
	// baseline and always fall through to the Runner. Tests use this to
	// assert live-parse behaviour; production leaves it false.
	DisableBaseline bool
	// DisableHealthLog, when true, suppresses parse-health log writes.
	// Tests use this to keep cache directory contents predictable.
	DisableHealthLog bool
	// StateDir, when non-empty, receives parse-health log entries
	// (spec §14.2). Defaults to Dir when empty.
	StateDir string
	// Debug, when non-nil, receives structured JSONL events for each
	// cache hit/miss and az subprocess invocation (spec §15.3).
	Debug *debug.Logger
}

// NewCache constructs a cache. A nil runner uses the real Azure CLI.
func NewCache(dir, azformVersion string, runner Runner) *Cache {
	if dir == "" {
		dir = DefaultCacheDir()
	}
	if azformVersion == "" {
		azformVersion = "dev"
	}
	if runner == nil {
		runner = NewExecRunner()
	}
	return &Cache{
		Dir:               dir,
		AzformVersion:     azformVersion,
		Runner:            runner,
		DetectEnvironment: DetectEnvironment,
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// DefaultCacheDir follows macOS and XDG cache conventions.
func DefaultCacheDir() string {
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Caches", "azform")
		}
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "azform")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "azform")
	}
	return filepath.Join(os.TempDir(), "azform-cache")
}

// Resolve returns cached metadata when available. A stale cache entry is
// returned immediately and includes a Refresh hook; it is never refreshed
// synchronously by this method.
func (c *Cache) Resolve(ctx context.Context, commandPath string) (*Result, error) {
	commandPath = NormalizeCommandPath(commandPath)
	if commandPath == "" {
		return nil, errors.New("metadata: empty command path")
	}
	start := time.Now()
	if c.Debug != nil {
		if env, envErr := c.detectEnvironment(); envErr == nil {
			c.Debug.Event("az_env", map[string]any{
				"path":         env.AZPath,
				"install_root": env.InstallPath,
			})
		}
	}

	result, err := c.resolveInner(ctx, commandPath)

	if c.Debug != nil && err == nil && result != nil {
		var health ParseHealth
		switch {
		case result.Command != nil:
			health = result.Command.ParseHealth
		case result.Group != nil:
			health = result.Group.ParseHealth
		}
		c.Debug.Event("cache.resolve", map[string]any{
			"command":     commandPath,
			"from_cache":  result.FromCache,
			"stale":       result.Stale,
			"duration_ms": time.Since(start).Milliseconds(),
			"params":      health.Params,
			"unparsed":    health.UnparsedLines,
			"sections_ok": health.SectionsOK,
		})
	}
	return result, err
}

func (c *Cache) resolveInner(ctx context.Context, commandPath string) (*Result, error) {
	env, envErr := c.detectEnvironment()
	if record, err := c.loadCommand(commandPath); err == nil {
		stale := c.commandStale(record, env, envErr)
		result := &Result{Kind: DocumentKindCommand, Stale: stale, FromCache: true, Command: record}
		if stale {
			result.Refresh = func(refreshCtx context.Context) error {
				_, err := c.refresh(refreshCtx, commandPath)
				return err
			}
		}
		return result, nil
	}
	if record, err := c.loadGroup(commandPath); err == nil {
		stale := c.groupStale(record, env, envErr)
		result := &Result{Kind: DocumentKindGroup, Stale: stale, FromCache: true, Group: record}
		if stale {
			result.Refresh = func(refreshCtx context.Context) error {
				_, err := c.refresh(refreshCtx, commandPath)
				return err
			}
		}
		return result, nil
	}

	// Embedded baseline (spec §3.4): return pre-baked metadata immediately
	// and trigger a background refresh so the on-disk cache catches up.
	if !c.DisableBaseline {
		if data, ok := baseline.Raw(Slug(commandPath)); ok {
			var rec CommandRecord
			if err := json.Unmarshal(data, &rec); err == nil && rec.SchemaVersion == SchemaVersion {
				result := &Result{
					Kind:      DocumentKindCommand,
					Stale:     true,
					FromCache: true,
					Command:   &rec,
				}
				result.Refresh = func(refreshCtx context.Context) error {
					_, err := c.refresh(refreshCtx, commandPath)
					return err
				}
				return result, nil
			}
		}
	}

	return c.refresh(ctx, commandPath)
}

func (c *Cache) refresh(ctx context.Context, commandPath string) (*Result, error) {
	if c.Debug != nil {
		c.Debug.Event("az_command", map[string]any{
			"command": commandPath,
			"args":    strings.Join(append(strings.Fields(commandPath), "--help", "--only-show-errors"), " "),
		})
	}
	azStart := time.Now()
	doc, azVersion, err := c.Runner.Resolve(ctx, strings.Split(commandPath, " "))
	azElapsed := time.Since(azStart)
	if c.Debug != nil {
		c.Debug.Event("az_command.done", map[string]any{
			"command":     commandPath,
			"duration_ms": azElapsed.Milliseconds(),
			"az_version":  azVersion,
			"ok":          err == nil,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("resolve az help for %q: %w", commandPath, err)
	}

	now := c.Now()
	c.recordHealth(commandPath, doc, now)
	result := &Result{Kind: doc.Kind, FromCache: false}
	switch doc.Kind {
	case DocumentKindCommand:
		if doc.Command.Command != commandPath {
			return nil, fmt.Errorf("metadata: requested %q but help describes %q", commandPath, doc.Command.Command)
		}
		record := &CommandRecord{
			SchemaVersion: SchemaVersion,
			Command:       doc.Command.Command,
			Summary:       doc.Command.Summary,
			AZVersion:     azVersion,
			AzformVersion: c.AzformVersion,
			Source:        SourceHelpParser,
			GeneratedAt:   now,
			ParseHealth:   doc.ParseHealth,
			Parameters:    doc.Command.Parameters,
		}
		if err := c.writeJSON(c.commandPath(commandPath), record); err != nil {
			return nil, fmt.Errorf("write command cache for %q: %w", commandPath, err)
		}
		result.Command = record
	case DocumentKindGroup:
		if doc.Group.Group != commandPath {
			return nil, fmt.Errorf("metadata: requested %q but help describes %q", commandPath, doc.Group.Group)
		}
		record := &GroupRecord{
			SchemaVersion: SchemaVersion,
			Group:         doc.Group.Group,
			Summary:       doc.Group.Summary,
			AZVersion:     azVersion,
			AzformVersion: c.AzformVersion,
			Source:        SourceHelpParser,
			GeneratedAt:   now,
			ParseHealth:   doc.ParseHealth,
			Subgroups:     doc.Group.Subgroups,
			Commands:      doc.Group.Commands,
		}
		if err := c.writeJSON(c.groupPath(commandPath), record); err != nil {
			return nil, fmt.Errorf("write group cache for %q: %w", commandPath, err)
		}
		result.Group = record
	default:
		return nil, fmt.Errorf("metadata: unsupported document kind %q", doc.Kind)
	}

	return result, nil
}

func (c *Cache) commandStale(record *CommandRecord, env Environment, envErr error) bool {
	return c.recordStale(record.GeneratedAt, record.AzformVersion, env, envErr)
}

func (c *Cache) groupStale(record *GroupRecord, env Environment, envErr error) bool {
	return c.recordStale(record.GeneratedAt, record.AzformVersion, env, envErr)
}

// recordStale is deliberately per-record. A global "environment was checked"
// file would make unrelated commands look fresh after only one command was
// revalidated, violating the lazy per-command strategy.
func (c *Cache) recordStale(generatedAt time.Time, azformVersion string, env Environment, envErr error) bool {
	if azformVersion != c.AzformVersion || envErr != nil || generatedAt.IsZero() {
		return true
	}
	if env.InstallModTime.IsZero() || generatedAt.Before(env.InstallModTime) {
		return true
	}
	return !env.ExtensionsModTime.IsZero() && generatedAt.Before(env.ExtensionsModTime)
}

// recordHealth appends a parse-health entry after a fresh parse (spec §14.2).
// Failures are silent: the diagnostic log is best-effort.
func (c *Cache) recordHealth(commandPath string, doc *Document, now time.Time) {
	if c.DisableHealthLog {
		return
	}
	stateDir := c.StateDir
	if stateDir == "" {
		stateDir = c.Dir
	}
	entry := diagnostics.Entry{Command: commandPath}
	switch doc.Kind {
	case DocumentKindCommand:
		entry.Params = doc.ParseHealth.Params
		entry.Unparsed = doc.ParseHealth.UnparsedLines
		entry.SectionsOK = doc.ParseHealth.SectionsOK
	case DocumentKindGroup:
		entry.SectionsOK = doc.ParseHealth.SectionsOK
	}
	_ = diagnostics.AppendHealth(stateDir, entry, now)
}

// NormalizeCommandPath collapses user whitespace while preserving Azure CLI's
// canonical command spelling.
func NormalizeCommandPath(path string) string {
	return strings.Join(strings.Fields(path), " ")
}

// Slug maps a command path to the cache file name from spec 3.2.
func Slug(commandPath string) string {
	commandPath = NormalizeCommandPath(commandPath)
	var b strings.Builder
	for _, r := range commandPath {
		switch {
		case r == ' ':
			b.WriteByte('_')
		case unicodeIsSlugRune(r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func unicodeIsSlugRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
}

func (c *Cache) commandPath(commandPath string) string {
	return filepath.Join(c.Dir, "commands", Slug(commandPath)+".json")
}

func (c *Cache) groupPath(commandPath string) string {
	return filepath.Join(c.Dir, "groups", Slug(commandPath)+".json")
}

func (c *Cache) loadCommand(commandPath string) (*CommandRecord, error) {
	var record CommandRecord
	if err := readJSON(c.commandPath(commandPath), &record); err != nil {
		return nil, fmt.Errorf("read command cache for %q: %w", commandPath, err)
	}
	if record.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("metadata: unsupported command cache schema %d", record.SchemaVersion)
	}
	if record.Command != commandPath {
		return nil, fmt.Errorf("metadata: command cache path mismatch %q != %q", record.Command, commandPath)
	}
	return &record, nil
}

func (c *Cache) loadGroup(commandPath string) (*GroupRecord, error) {
	var record GroupRecord
	if err := readJSON(c.groupPath(commandPath), &record); err != nil {
		return nil, fmt.Errorf("read group cache for %q: %w", commandPath, err)
	}
	if record.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("metadata: unsupported group cache schema %d", record.SchemaVersion)
	}
	if record.Group != commandPath {
		return nil, fmt.Errorf("metadata: group cache path mismatch %q != %q", record.Group, commandPath)
	}
	return &record, nil
}

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("metadata: decode %s: %w", path, err)
	}
	return nil
}

func (c *Cache) writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cache dir %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".azform-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", filepath.Dir(path), err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode json to %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func (c *Cache) detectEnvironment() (Environment, error) {
	if c.DetectEnvironment == nil {
		return DetectEnvironment()
	}
	return c.DetectEnvironment()
}

// DetectEnvironment resolves az and stats the install and extension roots.
// It performs no subprocess call and is safe on every form open.
func DetectEnvironment() (Environment, error) {
	azPath, err := exec.LookPath("az")
	if err != nil {
		return Environment{}, fmt.Errorf("metadata: az not found on PATH: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(azPath)
	if err == nil {
		azPath = resolved
	}
	installPath := azureCLIInstallRoot(azPath)
	installInfo, err := os.Stat(installPath)
	if err != nil {
		return Environment{}, fmt.Errorf("metadata: stat Azure CLI install root %s: %w", installPath, err)
	}

	env := Environment{
		AZPath:         azPath,
		InstallPath:    installPath,
		InstallModTime: installInfo.ModTime().UTC(),
	}
	env.ExtensionsPath = azureExtensionsDir()
	if info, err := os.Stat(env.ExtensionsPath); err == nil {
		env.ExtensionsModTime = info.ModTime().UTC()
	}
	return env, nil
}

func azureCLIInstallRoot(azPath string) string {
	clean := filepath.Clean(azPath)
	sep := string(filepath.Separator)
	for _, root := range []string{sep + filepath.Join("opt", "az"), sep + filepath.Join("lib64", "az")} {
		if clean == root || strings.HasPrefix(clean, root+sep) {
			return root
		}
	}
	if idx := strings.Index(clean, sep+filepath.Join("Cellar", "azure-cli")+sep); idx >= 0 {
		rest := clean[idx+len(sep+filepath.Join("Cellar", "azure-cli")+sep):]
		if slash := strings.Index(rest, sep); slash > 0 {
			return filepath.Join(clean[:idx], "Cellar", "azure-cli", rest[:slash])
		}
	}
	dir := filepath.Dir(clean)
	if filepath.Base(dir) == "bin" {
		return filepath.Dir(dir)
	}
	return dir
}

func azureExtensionsDir() string {
	configDir := os.Getenv("AZURE_CONFIG_DIR")
	if configDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configDir = filepath.Join(home, ".azure")
		}
	}
	if configDir == "" {
		return ""
	}
	return filepath.Join(configDir, "cliextensions")
}

// CacheSize returns aggregate cache size and record count for --doctor. It is
// deliberately best-effort and ignores unreadable entries.
func (c *Cache) CacheSize() (files int, bytes int64) {
	stats := CacheStats(c.Dir)
	return stats.Files, stats.Bytes
}

// Stats summarizes the on-disk cache for --doctor. It is deliberately
// best-effort and ignores unreadable entries.
func (c *Cache) Stats() Stats {
	return CacheStats(c.Dir)
}

// Stats summarizes the on-disk cache for --doctor. It is deliberately
// best-effort and ignores unreadable entries.
type Stats struct {
	Files       int
	Bytes       int64
	OldestEntry time.Time
	NewestEntry time.Time
}

// CacheStats walks <dir>/commands and <dir>/groups to produce the on-disk
// cache summary for --doctor. Unreadable entries are skipped silently.
func CacheStats(dir string) Stats {
	var s Stats
	for _, sub := range []string{filepath.Join(dir, "commands"), filepath.Join(dir, "groups")} {
		entries, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			s.Files++
			s.Bytes += info.Size()
			mtime := info.ModTime().UTC()
			if s.OldestEntry.IsZero() || mtime.Before(s.OldestEntry) {
				s.OldestEntry = mtime
			}
			if mtime.After(s.NewestEntry) {
				s.NewestEntry = mtime
			}
		}
	}
	return s
}
