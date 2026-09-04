package baseline_test

import (
	"encoding/json"
	"testing"

	"github.com/someson/azform/internal/metadata/baseline"
)

func TestRawHit(t *testing.T) {
	data, ok := baseline.Raw("storage_account_create")
	if !ok {
		t.Skip("baseline not generated; run cmd/gen-baseline first")
	}
	var probe struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatal(err)
	}
	if probe.Command != "storage account create" {
		t.Errorf("Command = %q", probe.Command)
	}
}

func TestRawMiss(t *testing.T) {
	if _, ok := baseline.Raw("does_not_exist"); ok {
		t.Error("expected miss for unknown slug")
	}
}

func TestSlugs(t *testing.T) {
	slugs := baseline.Slugs()
	if len(slugs) == 0 {
		t.Skip("baseline not generated")
	}
	for _, s := range slugs {
		if s == "" {
			t.Error("empty slug in baseline")
		}
	}
}
