// Package state manages persistent form state: drafts and (later) bindings.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultMaxEntries = 20
	DefaultTTL        = 7 * 24 * time.Hour
)

// DraftEntry is one saved form state.
type DraftEntry struct {
	Command  string            `json:"command"`
	Fields   map[string]string `json:"fields"`
	Disabled map[string]bool   `json:"disabled,omitempty"` // fields the user explicitly toggled off; absent entries default to enabled
	SavedAt  time.Time         `json:"saved_at"`
}

type draftFile struct {
	Entries []DraftEntry `json:"entries"`
}

// DraftStore persists and retrieves draft form states. The store serialises
// to Dir/drafts.json using temp+rename for crash safety.
type DraftStore struct {
	Dir        string
	MaxEntries int
	TTL        time.Duration
	Now        func() time.Time // injectable for tests; nil → time.Now().UTC()
}

// NewDraftStore returns a store rooted at dir.
func NewDraftStore(dir string) *DraftStore {
	return &DraftStore{Dir: dir, MaxEntries: DefaultMaxEntries, TTL: DefaultTTL}
}

// DefaultStateDir returns the XDG_STATE_HOME-based directory for azform.
func DefaultStateDir() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "azform")
	}
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "azform")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "azform")
	}
	return filepath.Join(os.TempDir(), "azform-state")
}

func (s *DraftStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *DraftStore) path() string { return filepath.Join(s.Dir, "drafts.json") }

// Save writes field values as the draft for command.
// Sensitive field names are excluded. Entries beyond MaxEntries are evicted
// oldest-first. The file is written atomically via temp+rename.
func (s *DraftStore) Save(command string, fields map[string]string) error {
	return s.SaveWithDisabled(command, fields, nil)
}

// SaveWithDisabled is Save plus a set of field names the user explicitly
// disabled. On Load, those fields come back Enabled=false so a binding
// the user toggled off stays off across an Esc-cancel / reopen cycle.
// Saving a nil disabled set is equivalent to Save.
func (s *DraftStore) SaveWithDisabled(command string, fields map[string]string, disabled map[string]bool) error {
	df, _ := s.read() // start from empty on first save or read error

	var kept []DraftEntry
	for _, e := range df.Entries {
		if e.Command != command {
			kept = append(kept, e)
		}
	}
	safe := make(map[string]string, len(fields))
	for k, v := range fields {
		if !IsSensitiveName(k) {
			safe[k] = v
		}
	}
	// Only persist disabled entries for fields that also have a value
	// (a field with no value and disabled = "user cleared this", which is
	// already conveyed by the absence of a value entry).
	safeDisabled := map[string]bool{}
	for k, on := range disabled {
		if on && safe[k] != "" {
			safeDisabled[k] = true
		}
	}
	kept = append(kept, DraftEntry{Command: command, Fields: safe, Disabled: safeDisabled, SavedAt: s.now()})

	limit := s.MaxEntries
	if limit <= 0 {
		limit = DefaultMaxEntries
	}
	if len(kept) > limit {
		kept = kept[len(kept)-limit:]
	}
	df.Entries = kept
	return s.write(df)
}

// Load returns the non-expired draft for command, if any. The disabled
// map carries field names the user explicitly toggled off before saving;
// callers should restore Enabled=false for those fields.
func (s *DraftStore) Load(command string) (map[string]string, map[string]bool, bool, error) {
	df, err := s.read()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	for _, e := range df.Entries {
		if e.Command != command {
			continue
		}
		if s.now().Sub(e.SavedAt) > ttl {
			return nil, nil, false, nil
		}
		return e.Fields, e.Disabled, true, nil
	}
	return nil, nil, false, nil
}

// Delete removes the draft for command (called on successful form submit).
func (s *DraftStore) Delete(command string) error {
	df, err := s.read()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var kept []DraftEntry
	for _, e := range df.Entries {
		if e.Command != command {
			kept = append(kept, e)
		}
	}
	df.Entries = kept
	return s.write(df)
}

func (s *DraftStore) read() (draftFile, error) {
	var df draftFile
	data, err := os.ReadFile(s.path())
	if err != nil {
		return df, fmt.Errorf("read draft file: %w", err)
	}
	if err := json.Unmarshal(data, &df); err != nil {
		return draftFile{}, fmt.Errorf("decode draft file: %w", err)
	}
	return df, nil
}

func (s *DraftStore) write(df draftFile) error {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return fmt.Errorf("create draft dir: %w", err)
	}
	tmp, err := os.CreateTemp(s.Dir, ".draft-*.tmp")
	if err != nil {
		return fmt.Errorf("create draft temp file: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(df); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode draft file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close draft temp file: %w", err)
	}
	if err := os.Rename(name, s.path()); err != nil {
		return fmt.Errorf("rename draft temp file: %w", err)
	}
	return nil
}

// IsSensitiveName reports whether name matches the sensitive-parameter patterns
// from spec 4.1 (vars) and 6.7 (drafts). Such fields are excluded from vars
// files and from draft/preset storage.
func IsSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, pat := range []string{
		"password", "passwd", "secret", "key", "token", "connection-string", "credential",
	} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}
