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
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeVarPick {
		t.Fatalf("mode after Ctrl+G = %v, want FormModeVarPick", f.Mode())
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

// TestVarPickerFixedFiveRows verifies interior height is 6 regardless
// of var count: 1 reserved filter line + 5 grid rows.
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
	// 5 grid rows + 1 reserved filter line = 6 interior rows.
	if count != 6 {
		t.Errorf("interior row count = %d, want 6 (5 grid + 1 filter)", count)
	}
}

// TestVarPickerScrollsHorizontally verifies pressing Right beyond the
// visible column window advances colOffset.
func TestVarPickerScrollsHorizontally(t *testing.T) {
	// Narrow terminal + many vars so cols > visibleCols.
	f := makePickerForm(t, genVarNames("PROJ_", 60), 40)
	// Cursor starts at (0,0). Press Right many times.
	for i := 0; i < 20; i++ {
		m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRight})
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

// typeInto pumps each rune through the form as a KeyRunes message.
func typeInto(t *testing.T, f ui.Form, s string) ui.Form {
	t.Helper()
	for _, r := range s {
		m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		f = m.(ui.Form)
	}
	return f
}

// TestVarPickerFilterNarrowsOnKeystroke verifies typing printable runes
// narrows the visible list case-insensitively and resets the cursor to
// (0, 0).
func TestVarPickerFilterNarrowsOnKeystroke(t *testing.T) {
	names := []string{"RG", "LOC", "MY_PATH", "PATTERN", "MY_HOME"}
	f := makePickerForm(t, names, 100)
	f = typeInto(t, f, "pat")
	col, row := f.PickerCursor()
	if col != 0 || row != 0 {
		t.Errorf("cursor after filter = (%d,%d), want (0,0)", col, row)
	}
	// Sum entries across all visible columns.
	got := []string{}
	for c := 0; c < f.PickerCols(); c++ {
		got = append(got, f.PickerColNames(c)...)
	}
	want := []string{"MY_PATH", "PATTERN"}
	if len(got) != len(want) {
		t.Fatalf("filtered names = %v, want %v", got, want)
	}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("filtered[%d] = %q, want %q", i, got[i], n)
		}
	}
}

// TestVarPickerFilterBackspace verifies Backspace pops one rune from
// the filter and re-widens the visible list.
func TestVarPickerFilterBackspace(t *testing.T) {
	names := []string{"MY_PATH", "PATTERN", "PAWN", "OTHER"}
	f := makePickerForm(t, names, 100)
	f = typeInto(t, f, "pat")
	m, _ := f.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	f = m.(ui.Form)
	got := []string{}
	for c := 0; c < f.PickerCols(); c++ {
		got = append(got, f.PickerColNames(c)...)
	}
	// "pa" matches MY_PATH, PATTERN, PAWN.
	want := []string{"MY_PATH", "PATTERN", "PAWN"}
	if len(got) != len(want) {
		t.Fatalf("after backspace, filtered = %v, want %v", got, want)
	}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("filtered[%d] = %q, want %q", i, got[i], n)
		}
	}
}

// TestVarPickerEscClearsFilterThenCloses verifies Esc first clears a
// non-empty filter (picker stays open) and only cancels on a second Esc.
func TestVarPickerEscClearsFilterThenCloses(t *testing.T) {
	f := makePickerForm(t, []string{"RG", "LOC"}, 100)
	f = typeInto(t, f, "x")
	// First Esc: clears filter, picker stays open. No cancel cmd.
	m, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	f = m.(ui.Form)
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(ui.VarPickerCancelledMsg); ok {
			t.Fatal("first Esc emitted VarPickerCancelledMsg; want filter-clear only")
		}
	}
	if f.Mode() != ui.FormModeVarPick {
		t.Fatalf("after first Esc, mode = %v, want FormModeVarPick", f.Mode())
	}
	// Second Esc: cancels. In the Form, the picker's cancel cmd is
	// delivered and mode transitions back to edit/list.
	_, cmd = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("second Esc should emit a cancel command")
	}
	msg := cmd()
	if _, ok := msg.(ui.VarPickerCancelledMsg); !ok {
		t.Fatalf("second Esc cmd = %T, want VarPickerCancelledMsg", msg)
	}
}

// TestVarPickerEnterOnEmptyMatchIsNoop verifies Enter is a no-op when
// no vars match the current filter.
func TestVarPickerEnterOnEmptyMatchIsNoop(t *testing.T) {
	f := makePickerForm(t, []string{"RG", "LOC"}, 100)
	f = typeInto(t, f, "zzzzz")
	m, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if cmd != nil {
		msg := cmd()
		if _, picked := msg.(ui.VarPickedMsg); picked {
			t.Fatal("Enter on empty filtered list emitted VarPickedMsg")
		}
		if _, cancelled := msg.(ui.VarPickerCancelledMsg); cancelled {
			t.Fatal("Enter on empty filtered list emitted VarPickerCancelledMsg")
		}
	}
	if f.Mode() != ui.FormModeVarPick {
		t.Fatalf("mode after no-op Enter = %v, want FormModeVarPick", f.Mode())
	}
}

// TestVarPickerFilterCaseInsensitive verifies filter matching ignores
// case and preserves original order.
func TestVarPickerFilterCaseInsensitive(t *testing.T) {
	names := []string{"MY_PATH", "my_path", "OTHER"}
	f := makePickerForm(t, names, 100)
	f = typeInto(t, f, "PA")
	got := []string{}
	for c := 0; c < f.PickerCols(); c++ {
		got = append(got, f.PickerColNames(c)...)
	}
	want := []string{"MY_PATH", "my_path"}
	if len(got) != len(want) {
		t.Fatalf("case-insensitive filter = %v, want %v", got, want)
	}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("filtered[%d] = %q, want %q", i, got[i], n)
		}
	}
}

// TestVarPickerFilterViewShowsInput verifies the rendered View() shows
// the current filter text on the reserved filter line.
func TestVarPickerFilterViewShowsInput(t *testing.T) {
	f := makePickerForm(t, []string{"MY_PATH", "PATTERN", "OTHER"}, 100)
	f = typeInto(t, f, "pa")
	out := f.View()
	if !strings.Contains(out, "filter:") {
		t.Errorf("View() missing `filter:` label; got:\n%s", out)
	}
	if !strings.Contains(out, "pa") {
		t.Errorf("View() missing filter text `pa`; got:\n%s", out)
	}
}
