package state_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/someson/azform/internal/state"
)

func newStore(t *testing.T) *state.DraftStore {
	t.Helper()
	return state.NewDraftStore(t.TempDir())
}

func TestDraftRoundTrip(t *testing.T) {
	s := newStore(t)
	fields := map[string]string{
		"--name":           "mystorage",
		"--resource-group": "$RG",
		"--sku":            "Standard_LRS",
	}
	if err := s.Save("storage account create", fields); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, ok, err := s.Load("storage account create")
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	for k, want := range fields {
		if got[k] != want {
			t.Errorf("field %s = %q, want %q", k, got[k], want)
		}
	}
}

func TestDraftDeleteOnDone(t *testing.T) {
	s := newStore(t)
	_ = s.Save("group create", map[string]string{"--name": "rg1"})
	if err := s.Delete("group create"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, _, ok, _ := s.Load("group create")
	if ok {
		t.Error("draft still present after Delete")
	}
}

func TestDraftTTLExpiry(t *testing.T) {
	s := newStore(t)
	s.Now = func() time.Time { return time.Now().Add(-8 * 24 * time.Hour) }
	_ = s.Save("vm create", map[string]string{"--name": "myvm"})
	s.Now = nil // real time
	_, _, ok, _ := s.Load("vm create")
	if ok {
		t.Error("expired draft should not be returned")
	}
}

func TestDraftSensitiveFieldsExcluded(t *testing.T) {
	s := newStore(t)
	fields := map[string]string{
		"--name":              "myapp",
		"--admin-password":    "hunter2",
		"--connection-string": "Endpoint=sb://...",
		"--secret-value":      "abc123",
	}
	_ = s.Save("webapp create", fields)
	got, _, ok, _ := s.Load("webapp create")
	if !ok {
		t.Fatal("draft not found")
	}
	if got["--name"] != "myapp" {
		t.Error("safe field --name missing")
	}
	for _, k := range []string{"--admin-password", "--connection-string", "--secret-value"} {
		if _, exists := got[k]; exists {
			t.Errorf("sensitive field %s was saved, should have been excluded", k)
		}
	}
}

func TestDraftMaxEntries(t *testing.T) {
	s := newStore(t)
	s.MaxEntries = 3
	for i := 0; i < 5; i++ {
		_ = s.Save(fmt.Sprintf("cmd%d create", i), map[string]string{"--name": "x"})
	}
	for i := 0; i < 2; i++ {
		_, _, ok, _ := s.Load(fmt.Sprintf("cmd%d create", i))
		if ok {
			t.Errorf("cmd%d should have been evicted", i)
		}
	}
	for i := 2; i < 5; i++ {
		_, _, ok, _ := s.Load(fmt.Sprintf("cmd%d create", i))
		if !ok {
			t.Errorf("cmd%d should be retained", i)
		}
	}
}

func TestDraftDisabledRoundRoll(t *testing.T) {
	s := newStore(t)
	values := map[string]string{
		"--name":           "mystorage",
		"--resource-group": "$RG",
		"--sku":            "Standard_LRS",
	}
	disabled := map[string]bool{
		"--sku": true, // user toggled --sku off before cancelling
	}
	if err := s.SaveWithDisabled("storage account create", values, disabled); err != nil {
		t.Fatalf("SaveWithDisabled: %v", err)
	}
	gotVals, gotDisabled, ok, err := s.Load("storage account create")
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if gotVals["--sku"] != "Standard_LRS" {
		t.Errorf("--sku value = %q, want Standard_LRS", gotVals["--sku"])
	}
	if !gotDisabled["--sku"] {
		t.Error("--sku should be marked disabled in draft")
	}
	if gotDisabled["--name"] {
		t.Error("--name should NOT be marked disabled")
	}
}

// TestDraftBackwardCompatWithoutDisabled covers an older drafts.json that
// has only the Fields map (no Disabled field). Loading it must still
// succeed and report Disabled == nil — callers treat that as "no
// explicit disabled markers" and apply their default (Enabled=true on
// restore when Value is non-empty).
func TestDraftBackwardCompatWithoutDisabled(t *testing.T) {
	dir := t.TempDir()
	// Write an old-format draft by hand.
	path := filepath.Join(dir, "drafts.json")
	old := `{"entries":[{"command":"group create","fields":{"--name":"rg1"},"saved_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s := state.NewDraftStore(dir)
	s.Now = func() time.Time { return time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) }
	vals, disabled, ok, err := s.Load("group create")
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if vals["--name"] != "rg1" {
		t.Errorf("--name = %q, want rg1", vals["--name"])
	}
	if disabled != nil {
		t.Errorf("disabled = %v, want nil (old-format draft has no Disabled map)", disabled)
	}
}

func TestIsSensitiveName(t *testing.T) {
	cases := []struct {
		name      string
		sensitive bool
	}{
		{"--name", false},
		{"--resource-group", false},
		{"--admin-password", true},
		{"--secret-value", true},
		{"--access-key", true},
		{"--sas-token", true},
		{"--connection-string", true},
		{"--certificate-credential", true},
		{"--PASSWORD", true},
	}
	for _, tc := range cases {
		got := state.IsSensitiveName(tc.name)
		if got != tc.sensitive {
			t.Errorf("IsSensitiveName(%q) = %v, want %v", tc.name, got, tc.sensitive)
		}
	}
}
