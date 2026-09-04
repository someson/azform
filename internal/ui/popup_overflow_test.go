package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/ui"
)

// TestEnumPopupBottomBorderNotClipped is the regression for the
// 2026-09-05 bug: with a short viewport the enum popup's bottom border
// was silently dropped when placed below the focused row. The form
// should grow to fit the popup instead.
func TestEnumPopupBottomBorderNotClipped(t *testing.T) {
	// Enum field is required so the initial cursor (0) lands on it.
	// A global param triggers grid mode (cols>=2) even with few ro fields.
	params := []metadata.Parameter{
		{Name: "--output", Required: true, TakesValue: true,
			ValueKind: metadata.ValueKindEnum,
			Choices:   []string{"json", "jsonc", "none", "table"},
			Group:     "Required Parameters"},
		{Name: "--debug", Global: true, Group: "Global Arguments"},
	}
	f := ui.NewForm("dummy", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: params, Summary: "."})
	f = m.(ui.Form)
	// Viewport height 2 — too small to hold any popup below the focused row.
	m, _ = f.Update(tea.WindowSizeMsg{Width: 160, Height: 2})
	f = m.(ui.Form)

	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeEnum {
		t.Fatalf("mode after Enter = %v, want FormModeEnum", f.Mode())
	}

	v := f.View()
	if !strings.Contains(v, "└") {
		t.Fatalf("popup bottom border ('└') missing from view:\n%s", v)
	}
	for _, choice := range []string{"json", "jsonc", "none", "table"} {
		if !strings.Contains(v, choice) {
			t.Errorf("choice %q missing from view:\n%s", choice, v)
		}
	}
}

// TestEnumPopupSlidingWindow caps the visible enum rows at
// maxPopupItems (7). With more than 7 choices the popup shows exactly
// 7 items and the top/bottom border carries a scroll glyph indicating
// hidden items in that direction.
func TestEnumPopupSlidingWindow(t *testing.T) {
	choices := []string{"c00", "c01", "c02", "c03", "c04", "c05", "c06", "c07", "c08", "c09"}
	params := []metadata.Parameter{
		{Name: "--pick", Required: true, TakesValue: true,
			ValueKind: metadata.ValueKindEnum, Choices: choices,
			Group: "Required Parameters"},
		{Name: "--debug", Global: true, Group: "Global Arguments"},
	}
	f := ui.NewForm("dummy", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: params, Summary: "."})
	f = m.(ui.Form)
	m, _ = f.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	f = m.(ui.Form)

	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeEnum {
		t.Fatalf("mode after Enter = %v, want FormModeEnum", f.Mode())
	}

	v := f.View()
	// Cursor starts at 0 → no clip at top, clip at bottom.
	if !strings.Contains(v, "↓") {
		t.Errorf("bottom scroll glyph '↓' missing (cursor at top, %d hidden below):\n%s", len(choices)-7, v)
	}
	// The last two choices must NOT be rendered yet.
	if strings.Contains(v, "c08") || strings.Contains(v, "c09") {
		t.Errorf("choices past the 7-item window should be hidden until scrolled; view:\n%s", v)
	}
	// Scroll the cursor to the end; window slides so top is now clipped.
	for i := 0; i < len(choices)-1; i++ {
		m, _ = f.Update(tea.KeyMsg{Type: tea.KeyDown})
		f = m.(ui.Form)
	}
	v = f.View()
	if !strings.Contains(v, "↑") {
		t.Errorf("top scroll glyph '↑' missing (cursor at bottom):\n%s", v)
	}
	if !strings.Contains(v, "c09") {
		t.Errorf("last choice 'c09' should be visible after scrolling to end:\n%s", v)
	}
}
