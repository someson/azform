package ui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/state"
	"github.com/someson/azform/internal/ui"
	"github.com/someson/azform/internal/validate"
	"github.com/someson/azform/internal/vars"
)

func typedValueParams() []metadata.Parameter {
	return []metadata.Parameter{
		{Name: "--name", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--resource-group", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
	}
}

func sendKey(t *testing.T, f ui.Form, k tea.KeyMsg) ui.Form {
	t.Helper()
	m, _ := f.Update(k)
	return m.(ui.Form)
}

// editField opens the text input on the focused field, replaces its whole
// content with value and commits with Enter.
func editField(t *testing.T, f ui.Form, value string) ui.Form {
	t.Helper()
	f = sendKey(t, f, tea.KeyMsg{Type: tea.KeyEnter})
	for len(f.TextInputValue()) > 0 {
		f = sendKey(t, f, tea.KeyMsg{Type: tea.KeyEnd})
		f = sendKey(t, f, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	f = typeInto(t, f, value)
	return sendKey(t, f, tea.KeyMsg{Type: tea.KeyEnter})
}

func submit(t *testing.T, f ui.Form) ui.Form {
	t.Helper()
	f = sendKey(t, f, tea.KeyMsg{Type: tea.KeyTab})
	return sendKey(t, f, tea.KeyMsg{Type: tea.KeyEnter})
}

// Editing a field used to keep whatever mode it had before, so a typed
// `$RG` in a literal field rendered as `'$RG'` (az got the four
// characters) and a var field edited to `my group` rendered unquoted and
// was word-split by the shell. The mode is now derived from the typed text.
func TestEditDerivesModeFromTypedValue(t *testing.T) {
	src := ui.Sources{
		Engine: validate.NewEngine(validate.BuiltinProvider{}),
		Vars: []vars.Variable{
			{Name: "RG", Value: "rg-from-shell"},
			{Name: "RESOURCE_GROUP", Value: "rg1"},
		},
	}
	f := ui.NewFormWithSources("group show", "/tmp/out.txt", t.TempDir(), "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: typedValueParams(), Summary: "."})
	f = m.(ui.Form)

	rg := f.Fields()[f.FieldIndex("--resource-group")]
	if rg.Mode != ui.FieldModeVar {
		t.Fatalf("precondition: --resource-group should be env-filled var mode, got %s", ui.ModeName(rg.Mode))
	}

	// --name is literal and focused first: type a var ref into it.
	f = editField(t, f, "$RG")
	name := f.Fields()[f.FieldIndex("--name")]
	if name.Mode != ui.FieldModeVar || name.VarValue != "rg-from-shell" {
		t.Errorf("typed $RG: mode=%s varValue=%q, want var/rg-from-shell", ui.ModeName(name.Mode), name.VarValue)
	}

	// --resource-group is var mode: overwrite it with a literal with a space.
	f = sendKey(t, f, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	f = editField(t, f, "my group")
	rg = f.Fields()[f.FieldIndex("--resource-group")]
	if rg.Mode != ui.FieldModeLiteral || rg.VarValue != "" {
		t.Errorf("typed literal: mode=%s varValue=%q, want literal/\"\"", ui.ModeName(rg.Mode), rg.VarValue)
	}

	f = submit(t, f)
	want := "az group show --name $RG --resource-group 'my group'"
	if f.Result() != want {
		t.Errorf("Result = %q, want %q (errorMsg=%q)", f.Result(), want, f.ErrorMsg())
	}
}

// A whole-value command substitution is shell syntax, not literal text.
func TestEditCommandSubstitutionIsVarMode(t *testing.T) {
	f := ui.NewFormWithSources("group show", "/tmp/out.txt", t.TempDir(), "test", nil, ui.Sources{})
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: typedValueParams(), Summary: "."})
	f = m.(ui.Form)
	f = editField(t, f, "$(cat name.txt)")
	name := f.Fields()[f.FieldIndex("--name")]
	if name.Mode != ui.FieldModeVar {
		t.Errorf("$(…) should be var mode, got %s", ui.ModeName(name.Mode))
	}
}

// The metadata default is the lowest pre-fill priority (spec §8.5), but it
// is applied first; fields holding only their default used to be skipped
// by every later stage, so Azure defaults, env matches and drafts never
// reached any param with a documented default.
func TestDefaultIsOverriddenByLowerStages(t *testing.T) {
	def := "eastus"
	params := []metadata.Parameter{
		{Name: "--name", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--location", TakesValue: true, ValueKind: metadata.ValueKindString, Default: &def, Group: "Optional Parameters"},
		{Name: "--tags", TakesValue: true, ValueKind: metadata.ValueKindString, Default: &def, Group: "Optional Parameters"},
	}

	dir := t.TempDir()
	if err := state.NewDraftStore(dir).Save("thing create", map[string]string{"--tags": "env=dev"}); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	src := ui.Sources{AzureDefaults: []vars.Variable{{Name: "location", Value: "westeurope"}}}
	f := ui.NewFormWithSources("thing create", "/tmp/out.txt", dir, "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: params, Summary: "."})
	f = m.(ui.Form)

	loc := f.Fields()[f.FieldIndex("--location")]
	if loc.Value != "westeurope" || loc.Source != ui.FieldSourceAzure {
		t.Errorf("--location = %q (source %s), want westeurope from azure defaults", loc.Value, loc.Source.Name())
	}
	tags := f.Fields()[f.FieldIndex("--tags")]
	if tags.Value != "env=dev" || tags.Source != ui.FieldSourceDraft {
		t.Errorf("--tags = %q (source %s), want env=dev from draft", tags.Value, tags.Source.Name())
	}
}

// Only enabled fields reach the command; a disabled field referencing an
// unset variable must not block Done.
func TestDisabledUndefinedVarDoesNotBlockDone(t *testing.T) {
	params := append(typedValueParams(), metadata.Parameter{
		Name: "--tags", TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Optional Parameters",
	})
	dir := t.TempDir()
	if err := state.NewDraftStore(dir).SaveWithDisabled("group show",
		map[string]string{"--name": "n", "--resource-group": "rg", "--tags": "$GONE"},
		map[string]bool{"--tags": true}); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	src := ui.Sources{Engine: validate.NewEngine(validate.BuiltinProvider{})}
	f := ui.NewFormWithSources("group show", "/tmp/out.txt", dir, "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: params, Summary: "."})
	f = submit(t, m.(ui.Form))
	if want := "az group show --name n --resource-group rg"; f.Result() != want {
		t.Errorf("Result = %q, want %q (errorMsg=%q)", f.Result(), want, f.ErrorMsg())
	}
}

// Ctrl+G inserts at the textinput cursor, which counts runes; splicing by
// byte offset panicked or split a character after non-ASCII text.
func TestVarPickerInsertAfterNonASCII(t *testing.T) {
	src := ui.Sources{Vars: []vars.Variable{{Name: "SUFFIX_X", Value: "x"}}}
	f := ui.NewFormWithSources("group show", "/tmp/out.txt", t.TempDir(), "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: typedValueParams(), Summary: "."})
	f = m.(ui.Form)
	f = sendKey(t, f, tea.KeyMsg{Type: tea.KeyEnter})
	f = typeInto(t, f, "é-")
	f = sendKey(t, f, tea.KeyMsg{Type: tea.KeyCtrlG})
	m, _ = f.Update(ui.VarPickedMsg{Name: "RG"})
	f = m.(ui.Form)
	if got := f.TextInputValue(); got != "é-$RG" {
		t.Errorf("input = %q, want %q", got, "é-$RG")
	}
}
