package state_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/someson/azform/internal/state"
)

func newBindingsStore(t *testing.T) (*state.BindingsStore, string) {
	t.Helper()
	dir := t.TempDir()
	return state.NewBindingsStore(dir), dir
}

func TestBindingKeyGlobal(t *testing.T) {
	if got := state.BindingKey("storage account create", "--resource-group"); got != "--resource-group" {
		t.Errorf("global key = %q, want \"--resource-group\"", got)
	}
}

func TestBindingKeyPerCommand(t *testing.T) {
	want := "storage account create\x00--name"
	if got := state.BindingKey("storage account create", "--name"); got != want {
		t.Errorf("per-command key = %q, want %q", got, want)
	}
}

func TestBindingsStoreLoadEmpty(t *testing.T) {
	s, _ := newBindingsStore(t)
	m, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Errorf("got %d entries, want 0", len(m))
	}
}

func TestBindingsStoreTouchAndLoad(t *testing.T) {
	s, _ := newBindingsStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }

	if err := s.Touch("--resource-group", state.Candidate{Name: "RG", Uses: 1, LastUsed: now}); err != nil {
		t.Fatal(err)
	}
	m, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	cands := m["--resource-group"]
	if len(cands) != 1 || cands[0].Name != "RG" {
		t.Errorf("got %+v, want one Candidate{Name:RG}", cands)
	}
}

func TestBindingsStoreTouchBumpsUses(t *testing.T) {
	s, _ := newBindingsStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if err := s.Touch("--resource-group", state.Candidate{Name: "RG", Uses: 1, LastUsed: now}); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := s.Load()
	cands := m["--resource-group"]
	if len(cands) != 1 || cands[0].Uses != 3 {
		t.Errorf("got %+v, want Uses=3", cands)
	}
}

func TestBindingsStoreMultipleCandidates(t *testing.T) {
	s, _ := newBindingsStore(t)
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	s.Now = func() time.Time { return t0 }
	if err := s.Touch("--resource-group", state.Candidate{Name: "RG", LastUsed: t0}); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return t1 }
	if err := s.Touch("--resource-group", state.Candidate{Name: "RG_PROD", LastUsed: t1}); err != nil {
		t.Fatal(err)
	}
	m, _ := s.Load()
	cands := m["--resource-group"]
	if len(cands) != 2 {
		t.Errorf("got %d candidates, want 2", len(cands))
	}
}

func TestBindingsStoreEviction(t *testing.T) {
	dir := t.TempDir()
	s := state.NewBindingsStore(dir)
	s.MaxEntries = 3
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		ts := t0.Add(time.Duration(i) * time.Minute)
		s.Now = func() time.Time { return ts }
		key := "--p" + string(rune('a'+i))
		if err := s.Touch(key, state.Candidate{Name: "v", LastUsed: ts}); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := s.Load()
	if len(m) > 3 {
		t.Errorf("got %d entries, want <=3 (cap)", len(m))
	}
}

func TestBindingsStoreCorruptFileGraceful(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bindings.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := state.NewBindingsStore(dir)
	m, err := s.Load()
	if err != nil {
		t.Fatalf("corrupt file should be tolerated, got error: %v", err)
	}
	if m == nil {
		t.Error("expected empty map, got nil")
	}
}

func TestBindingsStoreAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	s := state.NewBindingsStore(dir)
	if err := s.Touch("--resource-group", state.Candidate{Name: "RG", Uses: 1, LastUsed: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bindings.json")); err != nil {
		t.Errorf("bindings.json missing: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}

func TestBindingsStorePerCommandIsolation(t *testing.T) {
	s, _ := newBindingsStore(t)
	if err := s.Touch("storage account create\x00--name", state.Candidate{Name: "SA"}); err != nil {
		t.Fatal(err)
	}
	m, _ := s.Load()
	if _, ok := m["storage account create\x00--name"]; !ok {
		t.Errorf("per-command key missing")
	}
	if _, ok := m["--name"]; ok {
		t.Errorf("--name should NOT be in global key map")
	}
}

func TestBindingsJSONShape(t *testing.T) {
	s, _ := newBindingsStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	_ = s.Touch("--resource-group", state.Candidate{Name: "RG", Uses: 2, LastUsed: now})
	data, _ := os.ReadFile(filepath.Join(s.Dir, "bindings.json"))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("JSON parse failed: %v", err)
	}
	if _, ok := raw["--resource-group"]; !ok {
		t.Errorf("expected --resource-group key in JSON")
	}
}

func TestRankCandidatesRecentBeatsFrequent(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	// old-but-frequent (3 uses, 120 days ago) vs recent-single-use (1 use, today).
	// Decay half-life is 30 days, so old score ≈ 3 * 0.5^4 = 0.1875, recent ≈ 1.
	cands := []state.Candidate{
		{Name: "OLD", Uses: 3, LastUsed: now.AddDate(0, 0, -120)},
		{Name: "RG", Uses: 1, LastUsed: now},
	}
	got := state.RankCandidates(cands, now)
	if got[0].Name != "RG" {
		t.Errorf("winner = %q, want \"RG\" (recent single use beats old frequent)", got[0].Name)
	}
}

func TestRankCandidatesFrequentBeatsSlightlyOlder(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	// 10 uses 7 days ago (score ≈ 10 * 0.5^(7/30) ≈ 8.5)
	// vs 1 use today (score = 1). Frequent wins.
	cands := []state.Candidate{
		{Name: "NEW", Uses: 1, LastUsed: now},
		{Name: "FREQ", Uses: 10, LastUsed: now.AddDate(0, 0, -7)},
	}
	got := state.RankCandidates(cands, now)
	if got[0].Name != "FREQ" {
		t.Errorf("winner = %q, want \"FREQ\" (10 uses last week beats 1 use today)", got[0].Name)
	}
}

func TestRankCandidatesDoesNotMutateInput(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cands := []state.Candidate{
		{Name: "A", Uses: 1, LastUsed: now.AddDate(0, 0, -60)},
		{Name: "B", Uses: 1, LastUsed: now},
	}
	_ = state.RankCandidates(cands, now)
	if cands[0].Name != "A" || cands[1].Name != "B" {
		t.Errorf("input was mutated: %+v", cands)
	}
}

func TestRankCandidatesZeroTime(t *testing.T) {
	// Candidates with zero LastUsed fall back to Uses alone (no decay), so
	// higher-Uses wins deterministically without divide-by-zero or NaN.
	cands := []state.Candidate{
		{Name: "A", Uses: 1},
		{Name: "B", Uses: 5},
		{Name: "C", Uses: 3},
	}
	got := state.RankCandidates(cands, time.Time{})
	if got[0].Name != "B" || got[1].Name != "C" || got[2].Name != "A" {
		t.Errorf("order = [%s %s %s], want [B C A]", got[0].Name, got[1].Name, got[2].Name)
	}
}

func TestRankCandidatesTieBreakByLastUsed(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	// Same Uses, same age → equal score. Tiebreaker: LastUsed desc.
	cands := []state.Candidate{
		{Name: "OLDER", Uses: 2, LastUsed: now.AddDate(0, 0, -10)},
		{Name: "NEWER", Uses: 2, LastUsed: now.AddDate(0, 0, -5)},
	}
	got := state.RankCandidates(cands, now)
	if got[0].Name != "NEWER" {
		t.Errorf("tiebreak winner = %q, want \"NEWER\"", got[0].Name)
	}
}

func TestRankCandidatesEmpty(t *testing.T) {
	got := state.RankCandidates(nil, time.Now())
	if got == nil || len(got) != 0 {
		t.Errorf("nil input → %v, want empty slice", got)
	}
}
