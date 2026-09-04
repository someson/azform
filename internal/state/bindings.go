package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	DefaultBindingsMaxEntries = 500

	// bindingsHalfLifeDays: recency weight halves every 30 days. Chosen so a
	// single use last week outranks 3 uses from 3 months ago (spec §8.5).
	bindingsHalfLifeDays = 30.0
)

// PerCommandParams holds the params whose binding key is command-specific.
// Keep this list short and explicit; extend as real usage shows the need
// (spec §8.2). Resource-identifying params like --name live here so that
// a storage account binding doesn't leak into vm create.
var PerCommandParams = map[string]bool{
	"--name": true,
}

// BindingKey returns the storage key for a (command, param) pair. Most
// params share a global key (canonical name); resource-identifying params
// are scoped to the command so the same name can mean different things
// across commands (spec §8.2).
func BindingKey(command, paramName string) string {
	if PerCommandParams[paramName] {
		return command + "\x00" + paramName
	}
	return paramName
}

// Candidate is one remembered var-or-literal for a binding key.
type Candidate struct {
	Name     string    `json:"name"`
	Value    string    `json:"value,omitempty"`
	Uses     int       `json:"uses"`
	LastUsed time.Time `json:"last_used"`
}

// BindingsStore persists remembered param→var bindings to bindings.json in
// the state directory (NOT the cache dir; spec §8.2). The file survives
// metadata cache invalidation.
type BindingsStore struct {
	Dir        string
	MaxEntries int
	Now        func() time.Time
}

// NewBindingsStore returns a store rooted at dir.
func NewBindingsStore(dir string) *BindingsStore {
	return &BindingsStore{Dir: dir, MaxEntries: DefaultBindingsMaxEntries}
}

func (s *BindingsStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *BindingsStore) path() string { return filepath.Join(s.Dir, "bindings.json") }

// Load returns the bindings map. Missing or corrupt files yield an empty
// map and no error — corrupted bindings must not block the form.
func (s *BindingsStore) Load() (map[string][]Candidate, error) {
	out := map[string][]Candidate{}
	data, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("read bindings file: %w", err)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		// Corrupt file: return empty map, don't propagate — the store is
		// self-healing since the next write replaces the file atomically.
		return map[string][]Candidate{}, nil //nolint:nilerr // intentional recovery
	}
	if out == nil {
		out = map[string][]Candidate{}
	}
	return out, nil
}

// Lookup returns the candidates for the given key.
func (s *BindingsStore) Lookup(key string) []Candidate {
	m, _ := s.Load()
	return m[key]
}

// RankCandidates returns cands sorted by a "recent + used often" score,
// highest first. Score = Uses * 0.5^(ageDays/halfLife); ties broken by
// LastUsed desc, then by insertion order. The input slice is not modified.
// When LastUsed is zero (never touched) or now is zero, decay is skipped
// and score = Uses. Used by the TUI to pick the best pre-fill candidate
// among many bindings for the same param (spec §8.5).
func RankCandidates(cands []Candidate, now time.Time) []Candidate {
	out := make([]Candidate, len(cands))
	copy(out, cands)
	sort.SliceStable(out, func(i, j int) bool {
		si := candidateScore(out[i], now)
		sj := candidateScore(out[j], now)
		if si != sj {
			return si > sj
		}
		return out[i].LastUsed.After(out[j].LastUsed)
	})
	return out
}

func candidateScore(c Candidate, now time.Time) float64 {
	uses := float64(c.Uses)
	if uses <= 0 {
		uses = 1
	}
	if c.LastUsed.IsZero() || now.IsZero() {
		return uses
	}
	ageDays := now.Sub(c.LastUsed).Hours() / 24.0
	if ageDays < 0 {
		ageDays = 0
	}
	return uses * math.Pow(0.5, ageDays/bindingsHalfLifeDays)
}

// Touch records a use of candidate c at key. If an existing candidate
// matches by Name (and Value for literals), its Uses and LastUsed are
// bumped; otherwise the candidate is appended. After write, entries
// beyond MaxEntries are evicted by oldest LastUsed.
func (s *BindingsStore) Touch(key string, c Candidate) error {
	c.LastUsed = s.now()
	m, _ := s.Load()

	list := m[key]
	found := false
	for i := range list {
		if list[i].Name == c.Name && list[i].Value == c.Value {
			list[i].Uses++
			list[i].LastUsed = c.LastUsed
			found = true
			break
		}
	}
	if !found {
		if c.Uses == 0 {
			c.Uses = 1
		}
		list = append(list, c)
	}
	m[key] = list

	limit := s.MaxEntries
	if limit <= 0 {
		limit = DefaultBindingsMaxEntries
	}
	if len(m) > limit {
		type kv struct {
			key string
			t   time.Time
		}
		all := make([]kv, 0, len(m))
		for k, cs := range m {
			if len(cs) > 0 {
				all = append(all, kv{k, cs[len(cs)-1].LastUsed})
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
		drop := len(all) - limit
		for i := 0; i < drop; i++ {
			delete(m, all[i].key)
		}
	}

	return s.write(m)
}

func (s *BindingsStore) write(m map[string][]Candidate) error {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", s.Dir, err)
	}
	tmp, err := os.CreateTemp(s.Dir, ".bindings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path())
}
