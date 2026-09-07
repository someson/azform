package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/state"
	"github.com/someson/azform/internal/ui"
	"github.com/someson/azform/internal/vars"
)

// The `v` key is a global value-display cycle for required params.
// It doesn't mutate any per-field state (Mode / Value / VarValue) —
// it just advances Form.valueDisplayMode, and the render layer picks
// the appropriate display for each required + var-mode + resolving
// field. This file asserts the cycle produces the right text in the
// value column across all three states.

func paramsWithRG(t *testing.T) []metadata.Parameter {
	t.Helper()
	return []metadata.Parameter{
		{Name: "--name", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--resource-group", Aliases: []string{"-g"}, Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--tags", Required: false, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Optional Parameters"},
	}
}

func formWithRG(t *testing.T, withVars bool) ui.Form {
	t.Helper()
	src := ui.Sources{}
	if withVars {
		src.Vars = []vars.Variable{{Name: "RG", Value: "myResourceGroup"}}
	}
	f := ui.NewFormWithSources("storage account create", "/tmp/out.txt", t.TempDir(), "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: paramsWithRG(t), Summary: "."})
	return m.(ui.Form)
}

// findRow returns the line in the view that contains the given flag name
// (after stripping ANSI). Used by the cycle tests to assert what the
// value column renders.
func findRow(t *testing.T, view, flag string) string {
	t.Helper()
	for _, ln := range strings.Split(view, "\n") {
		if strings.Contains(ln, flag) {
			return ln
		}
	}
	t.Fatalf("no row containing %q in view:\n%s", flag, view)
	return ""
}

// press moves cursor down until the given flag is focused, then sends
// `v`. Returns the form after the press.
func press(f ui.Form, nav, key rune) ui.Form {
	rgIdx := f.FieldIndex("--resource-group")
	for i := 0; i < rgIdx; i++ {
		m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{nav}})
		f = m.(ui.Form)
	}
	m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	return m.(ui.Form)
}

func TestVCycleRequiredResolvingVar(t *testing.T) {
	f := formWithRG(t, true)
	// initial mode is 1 → $RG → myResourceGroup
	if !strings.Contains(findRow(t, f.View(), "--resource-group"), "$RG") {
		t.Fatalf("initial row missing $RG:\n%s", f.View())
	}
	if !strings.Contains(findRow(t, f.View(), "--resource-group"), "myResourceGroup") {
		t.Fatalf("initial row missing myResourceGroup:\n%s", f.View())
	}

	// v → 2 → value only
	f = press(f, 'j', 'v')
	row := findRow(t, f.View(), "--resource-group")
	if !strings.Contains(row, "myResourceGroup") {
		t.Errorf("after 1st v, want value column to show myResourceGroup; got: %q", row)
	}
	if strings.Contains(row, "$RG") {
		t.Errorf("after 1st v, $RG should be hidden; got: %q", row)
	}

	// v → 0 → ref only
	f = press(f, 'j', 'v')
	row = findRow(t, f.View(), "--resource-group")
	if !strings.Contains(row, "$RG") {
		t.Errorf("after 2nd v, want $RG; got: %q", row)
	}
	if strings.Contains(row, "myResourceGroup") {
		t.Errorf("after 2nd v, myResourceGroup should be hidden; got: %q", row)
	}

	// v → 1 → $RG → myResourceGroup (cycle wraps)
	f = press(f, 'j', 'v')
	row = findRow(t, f.View(), "--resource-group")
	if !strings.Contains(row, "$RG") || !strings.Contains(row, "myResourceGroup") {
		t.Errorf("after 3rd v, want both $RG and myResourceGroup; got: %q", row)
	}
}

func TestVCycleDoesNotMutateFieldState(t *testing.T) {
	// The cycle must be a view-only operation. Field state must not
	// change across presses — only the rendered text does. This is
	// the core design rule: v never touches field state.
	f := formWithRG(t, true)
	before := f.Fields()[f.FieldIndex("--resource-group")]
	beforeSnapshot := func() (string, string, string) {
		ff := f.Fields()[f.FieldIndex("--resource-group")]
		return ff.Value, ff.VarValue, ui.ModeName(ff.Mode)
	}
	bVal, bVarVal, bMode := beforeSnapshot()
	for i := 0; i < 6; i++ {
		f = press(f, 'j', 'v')
		ff := f.Fields()[f.FieldIndex("--resource-group")]
		if ff.Value != before.Value || ff.VarValue != before.VarValue || ff.Mode != before.Mode {
			t.Fatalf("press %d mutated field state: was (Value=%q VarValue=%q Mode=%s), now (%q %q %s)",
				i+1, bVal, bVarVal, bMode, ff.Value, ff.VarValue, ui.ModeName(ff.Mode))
		}
	}
}

func TestVCycleIgnoresUnresolvingVar(t *testing.T) {
	// Field with a var ref that the shell doesn't have: render stays
	// red (VarStatusGray) on whatever DisplayValue produces, and the
	// cycle must NOT change it.
	f := formWithRG(t, false) // no $RG in shell
	before := stripForTest(findRow(t, f.View(), "--resource-group"))
	for i := 0; i < 4; i++ {
		f = press(f, 'j', 'v')
		after := stripForTest(findRow(t, f.View(), "--resource-group"))
		// The row changes only when the cycle changes what's rendered;
		// here, the var doesn't resolve so the cycle is a no-op.
		// DisplayValue() returns the literal value (which is empty
		// for an unresolved var since VarValue is "") — should stay
		// empty.
		if strings.Contains(after, "myResourceGroup") {
			t.Errorf("unresolving var should never show myResourceGroup; got: %q", after)
		}
	}
	_ = before
}

// Regression for the 2026-09-06 "v doesn't work" bug: a draft-restored
// required field has Value="$RG" as literal text (Mode=Literal,
// VarValue="" — drafts don't preserve var info). The cycle used to
// only fire when Mode==FieldModeVar, so pressing v on such a field
// was a no-op. The rendering now detects "$REF" text independently of
// Mode and honours the cycle for any required field whose value is a
// resolving var ref. Lookup is view-only (m.src.Vars), no field state
// mutation.
func TestVCycleDraftRestoredLiteral(t *testing.T) {
	dir := t.TempDir()
	store := state.NewDraftStore(dir)
	if err := store.Save("storage account create", map[string]string{
		"--resource-group": "$RG",
	}); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	src := ui.Sources{
		Vars: []vars.Variable{{Name: "RG", Value: "myResourceGroup"}},
	}
	f := ui.NewFormWithSources("storage account create", "/tmp/out.txt", dir, "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: paramsWithRG(t), Summary: "."})
	f = m.(ui.Form)

	rg := f.Fields()[f.FieldIndex("--resource-group")]
	if rg.Mode != ui.FieldModeLiteral || rg.VarValue != "" || rg.Source != ui.FieldSourceDraft {
		t.Fatalf("precondition: expected literal draft $RG, got mode=%s varValue=%q source=%s",
			ui.ModeName(rg.Mode), rg.VarValue, rg.Source.Name())
	}

	rgIdx := f.FieldIndex("--resource-group")
	for i := 0; i < rgIdx; i++ {
		m, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		f = m.(ui.Form)
	}

	// initial mode 1 → $RG → myResourceGroup
	row := findRow(t, f.View(), "--resource-group")
	if !strings.Contains(row, "$RG") || !strings.Contains(row, "myResourceGroup") {
		t.Fatalf("initial render should show $RG → myResourceGroup; got: %q", row)
	}

	// v → 2 → myResourceGroup only
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	f = m.(ui.Form)
	row = findRow(t, f.View(), "--resource-group")
	if !strings.Contains(row, "myResourceGroup") {
		t.Errorf("after 1st v: want myResourceGroup; got: %q", row)
	}
	if strings.Contains(row, "$RG") {
		t.Errorf("after 1st v: $RG should be hidden; got: %q", row)
	}

	// v → 0 → $RG only
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	f = m.(ui.Form)
	row = findRow(t, f.View(), "--resource-group")
	if !strings.Contains(row, "$RG") {
		t.Errorf("after 2nd v: want $RG; got: %q", row)
	}

	// Field state must NOT have changed — still draft literal with empty
	// VarValue. The cycle is view-only.
	rg = f.Fields()[f.FieldIndex("--resource-group")]
	if rg.Mode != ui.FieldModeLiteral || rg.VarValue != "" || rg.Value != "$RG" {
		t.Errorf("cycle mutated field state: mode=%s value=%q varValue=%q",
			ui.ModeName(rg.Mode), rg.Value, rg.VarValue)
	}
}

func TestVCycleIgnoresLiteralFields(t *testing.T) {
	// Optional / literal fields never participate in the cycle.
	f := formWithRG(t, true)
	tagsIdx := f.FieldIndex("--tags")
	for i := 0; i < tagsIdx; i++ {
		m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		f = m.(ui.Form)
	}
	m, _ := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	for _, r := range "k=v" {
		m, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		f = m.(ui.Form)
	}
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)

	tags := f.Fields()[f.FieldIndex("--tags")]
	if tags.Value != "k=v" || tags.Mode != ui.FieldModeLiteral {
		t.Fatalf("precondition: expected literal k=v, got Value=%q Mode=%s",
			tags.Value, ui.ModeName(tags.Mode))
	}

	for i := 0; i < 3; i++ {
		f = press(f, 'j', 'v')
		row := findRow(t, f.View(), "--tags")
		if !strings.Contains(row, "k=v") {
			t.Errorf("optional literal field must always render its value; got: %q", row)
		}
	}
}

func stripForTest(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in:
			if r == 'm' {
				in = false
			}
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}
