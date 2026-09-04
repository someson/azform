package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/someson/azform/internal/ui"
	"github.com/someson/azform/internal/validate"
	"github.com/someson/azform/internal/vars"
)

// makePickerForm builds a Form with the given var list, opens the var
// picker on the first field's edit mode, and returns the ready Form.
func makePickerForm(t *testing.T, names []string, termWidth int) ui.Form {
	t.Helper()
	dir := t.TempDir()
	vs := make([]vars.Variable, len(names))
	for i, n := range names {
		vs[i] = vars.Variable{Name: n, Value: "v"}
	}
	src := ui.Sources{
		Engine: validate.NewEngine(validate.BuiltinProvider{}),
		Vars:   vs,
	}
	f := ui.NewFormWithSources("storage account create", "/tmp/out.txt", dir, "test", nil, src)
	m, _ := f.Update(tea.WindowSizeMsg{Width: termWidth, Height: 40})
	m, _ = m.(ui.Form).Update(ui.MetadataLoadedMsg{Params: testParams, Summary: "."})
	f = m.(ui.Form)
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeVarPick {
		t.Fatalf("mode after Ctrl+V = %v, want FormModeVarPick", f.Mode())
	}
	return f
}

func genVarNames(prefix string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = prefix
		// Use different letters to avoid suffix heuristics.
		out[i] += string(rune('A'+(i/26)%26)) + string(rune('A'+i%26))
	}
	return out
}

// TestVarPickerUnifiedBorder verifies the picker renders as one bordered
// box with exactly one top-left and one bottom-left corner (not per-col).
func TestVarPickerUnifiedBorder(t *testing.T) {
	f := makePickerForm(t, genVarNames("PROJ_", 15), 100)
	out := f.View()
	if got := strings.Count(out, "┌"); got != 1 {
		t.Errorf("expected exactly 1 `┌` in output, got %d", got)
	}
	if got := strings.Count(out, "└"); got != 1 {
		t.Errorf("expected exactly 1 `└` in output, got %d", got)
	}
}

// TestVarPickerFullWidth verifies the popup box spans the terminal width.
func TestVarPickerFullWidth(t *testing.T) {
	const termWidth = 100
	f := makePickerForm(t, genVarNames("PROJ_", 15), termWidth)
	out := f.View()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "┌") || strings.HasPrefix(line, "└") {
			if w := lipgloss.Width(line); w != termWidth {
				t.Errorf("border line width = %d, want %d (%q)", w, termWidth, line)
			}
		}
	}
}

// TestVarPickerFixedFiveRows verifies interior height is 5 regardless of
// var count.
func TestVarPickerFixedFiveRows(t *testing.T) {
	f := makePickerForm(t, genVarNames("PROJ_", 20), 100)
	out := f.View()
	// Interior lines are `│…│` — count them.
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "│") && strings.HasSuffix(strings.TrimRight(line, " "), "│") {
			count++
		}
	}
	if count != 5 {
		t.Errorf("interior row count = %d, want 5", count)
	}
}

// TestVarPickerScrollsHorizontally verifies pressing `l` beyond the
// visible column window advances colOffset.
func TestVarPickerScrollsHorizontally(t *testing.T) {
	// Narrow terminal + many vars so cols > visibleCols.
	f := makePickerForm(t, genVarNames("PROJ_", 60), 40)
	// Grab initial visibility. Access via accessor by calling on
	// the underlying picker via PickerCols / cursor.
	// Cursor starts at (0,0). Press 'l' many times.
	for i := 0; i < 20; i++ {
		m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
		f = m.(ui.Form)
	}
	col, _ := f.PickerCursor()
	if col == 0 {
		t.Fatalf("cursor colIdx did not advance")
	}
	out := f.View()
	// Overflow indicator should be present.
	if !strings.Contains(out, "/") {
		t.Errorf("expected scroll indicator with `/` in top border")
	}
}

// TestVarPickerScrollIndicator verifies the top border contains a
// N-M/T scroll fragment when the picker overflows.
func TestVarPickerScrollIndicator(t *testing.T) {
	f := makePickerForm(t, genVarNames("PROJ_", 60), 40)
	out := f.View()
	// Extract lines starting with `┌`.
	var topLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "┌") {
			topLine = line
			break
		}
	}
	if topLine == "" {
		t.Fatal("no top border found")
	}
	// Should contain "/12" style (60 vars / 5 rows = 12 cols).
	if !strings.Contains(topLine, "/12") {
		t.Errorf("top border missing `/12` total: %q", topLine)
	}
	if !strings.Contains(topLine, "1-") {
		t.Errorf("top border missing `1-` range start: %q", topLine)
	}
}
