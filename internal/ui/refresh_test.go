package ui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
)

// runCmd executes cmd and, for a batch, every command inside it, returning
// all produced messages.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// The cache returns stale entries (and the embedded baseline) with a
// Refresh hook but never runs it. Nothing in the form ran it either, so a
// stale entry stayed stale — banner included — forever.
func TestStaleMetadataTriggersBackgroundRefresh(t *testing.T) {
	calls := 0
	f := NewForm("group show", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, cmd := f.Update(MetadataLoadedMsg{
		Params:      []metadata.Parameter{{Name: "--name", TakesValue: true, ValueKind: metadata.ValueKindString}},
		Stale:       true,
		StaleReason: "az was upgraded since caching",
		refresh: func(context.Context) error {
			calls++
			return nil
		},
	})
	msgs := runCmd(cmd)
	if calls != 1 {
		t.Fatalf("refresh called %d times, want 1", calls)
	}
	var got bool
	for _, msg := range msgs {
		if _, ok := msg.(metadataRefreshedMsg); ok {
			got = true
			if _, next := m.Update(msg); next != nil {
				t.Errorf("metadataRefreshedMsg should not schedule more work")
			}
		}
	}
	if !got {
		t.Errorf("no metadataRefreshedMsg among %v", msgs)
	}
}

func TestFreshMetadataDoesNotRefresh(t *testing.T) {
	calls := 0
	f := NewForm("group show", "/tmp/out.txt", t.TempDir(), "test", nil)
	_, cmd := f.Update(MetadataLoadedMsg{
		Params:  []metadata.Parameter{{Name: "--name", TakesValue: true, ValueKind: metadata.ValueKindString}},
		refresh: func(context.Context) error { calls++; return nil },
	})
	runCmd(cmd)
	if calls != 0 {
		t.Errorf("refresh called %d times for a fresh entry, want 0", calls)
	}
}
