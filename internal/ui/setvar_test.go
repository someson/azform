package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/ui"
	"github.com/someson/azform/internal/validate"
	"github.com/someson/azform/internal/vars"
)

// typeRunes pumps each rune of s into the form as a separate KeyMsg,
// which is the realistic key sequence the textinput component sees from
// a real user. Using one message per rune keeps cursor positions and
// character widths sane across the bubbles/textinput internals.
func typeRunes(t *testing.T, f ui.Form, s string) ui.Form {
	t.Helper()
	for _, r := range s {
		m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		f = m.(ui.Form)
	}
	return f
}

// TestSetVarPopupOpens covers the entry point: pressing lowercase 'g'
// from list mode must switch the form into FormModeSetVar with the
// input focused, and the rendered view must advertise the popup (the
// "set shell variable" label is on the prompt so a user can tell the
// mode has changed even before the cursor blinks).
func TestSetVarPopupOpens(t *testing.T) {
	f := loadedForm(t)
	m, _ := f.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	f = m.(ui.Form)

	m, cmd := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeSetVar {
		t.Fatalf("mode after g = %v, want FormModeSetVar", f.Mode())
	}
	if cmd == nil {
		t.Error("g should return a focus cmd (textinput.Focus) so the cursor blinks")
	}
	view := f.View()
	if !strings.Contains(view, "set shell variable") {
		t.Errorf("view should contain the popup label; view:\n%s", view)
	}
}

// TestSetVarAcceptsNameEqValue is the happy path: the user types
// `RG=foo`, presses Enter, the popup stays open (multi-line batch
// entry), and a single export line lands in pendingExports with the
// value single-quoted. The CLI writes the file on Done.
func TestSetVarAcceptsNameEqValue(t *testing.T) {
	f := loadedForm(t)
	m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)
	f = typeRunes(t, f, "RG=foo")
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeSetVar {
		t.Errorf("popup should stay open after Enter on a valid line; mode = %v", f.Mode())
	}
	got := f.PendingEnvExports()
	want := []string{"export RG='foo'"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("pendingExports = %v, want %v", got, want)
	}
	if v := f.SetVarInputValue(); v != "" {
		t.Errorf("input should be cleared after a successful commit, got %q", v)
	}
}

// TestSetVarAcceptsBareName covers the second entry shape: the user
// types just a name (no `=`) to re-export the current shell value.
// When the widget has the var (via Sources.Vars), the export line uses
// the live value; when it doesn't, the hint explains why and the input
// stays for the user to retry.
func TestSetVarAcceptsBareName(t *testing.T) {
	src := ui.Sources{
		Engine: validate.NewEngine(validate.BuiltinProvider{}),
		Vars: []vars.Variable{
			{Name: "RG", Value: "myResourceGroup"},
		},
	}
	f := ui.NewFormWithSources("storage account create", "/tmp/out.txt", t.TempDir(), "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: testParams, Summary: "."})
	f = m.(ui.Form)
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)
	f = typeRunes(t, f, "RG")
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	got := f.PendingEnvExports()
	want := []string{"export RG='myResourceGroup'"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("pendingExports = %v, want %v", got, want)
	}

	// Now the missing-var branch: query a name the widget never saw.
	f = typeRunes(t, f, "UNKNOWN")
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if got := f.Mode(); got != ui.FormModeSetVar {
		t.Errorf("popup should stay open on missing var; mode = %v", got)
	}
	if hint := f.SetVarHint(); !strings.Contains(hint, "UNKNOWN") || !strings.Contains(hint, "not set") {
		t.Errorf("hint = %q, want it to name the missing var", hint)
	}
	if v := f.SetVarInputValue(); v != "UNKNOWN" {
		t.Errorf("input should be kept after a failed parse, got %q", v)
	}
	if got := f.PendingEnvExports(); len(got) != 1 {
		t.Errorf("pendingExports = %v, want length-1 (the prior RG export only)", got)
	}
}

// TestSetVarRejectsInvalidName locks down the three error cases the
// plan calls out: a digit-leading name, an empty name (`=foo`), and a
// name with a space. Each one must surface a hint and leave pendingExports
// untouched — never silently emit an `export =foo` or similar nonsense.
func TestSetVarRejectsInvalidName(t *testing.T) {
	cases := []struct {
		input string
		why   string
	}{
		{"1FOO=bar", "digit prefix"},
		{"=bar", "empty name"},
		{"FOO BAR=baz", "space in name"},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			f := loadedForm(t)
			m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
			f = m.(ui.Form)
			f = typeRunes(t, f, tc.input)
			m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			f = m.(ui.Form)
			if f.Mode() != ui.FormModeSetVar {
				t.Errorf("popup should stay open on invalid name; mode = %v", f.Mode())
			}
			if hint := f.SetVarHint(); !strings.Contains(strings.ToLower(hint), "invalid var name") {
				t.Errorf("hint = %q, want it to mention 'invalid var name'", hint)
			}
			if got := f.PendingEnvExports(); len(got) != 0 {
				t.Errorf("pendingExports = %v, want empty on parse error", got)
			}
		})
	}
}

// TestSetVarQuotesSingleQuote locks down the POSIX-portable quoting
// for values containing single quotes. The `'\”` escape (close-quote,
// literal quote, reopen-quote) is the only form that's identical in
// zsh, bash, and dash. Without this, a value like `it's` would either
// break the widget's `eval` step or land in the shell with a
// syntax error.
func TestSetVarQuotesSingleQuote(t *testing.T) {
	f := loadedForm(t)
	m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)
	f = typeRunes(t, f, "TOKEN=it's")
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	got := f.PendingEnvExports()
	// quoteForShell wraps in `'...'`, replacing each `'` with `'\'''`
	// (close, escape, reopen). For `it's` this gives the literal
	// sequence below — note the doubled `'\'\'` boundary.
	want := []string{`export TOKEN='it'\'''s'`}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("pendingExports = %q, want %q", got[0], want[0])
	}
}

// TestSetVarEmptyEnterCloses covers the explicit close path: an empty
// input followed by Enter leaves FormModeSetVar and returns to the
// field list. pendingExports survives (the CLI flushes them on Done
// when the user presses the Done button); the form just stops
// listening to the popup.
func TestSetVarEmptyEnterCloses(t *testing.T) {
	f := loadedForm(t)
	m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)

	// Queue one export first so we can prove the close leaves it intact.
	f = typeRunes(t, f, "FOO=bar")
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if got := f.PendingEnvExports(); len(got) != 1 {
		t.Fatalf("setup pendingExports = %v, want 1 entry", got)
	}

	// Empty Enter closes the popup.
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeList {
		t.Errorf("empty Enter should close popup; mode = %v", f.Mode())
	}
	if got := f.PendingEnvExports(); len(got) != 1 || got[0] != "export FOO='bar'" {
		t.Errorf("pendingExports after close = %v, want [export FOO='bar'] (close must not drop the queue)", got)
	}
}

// TestSetVarMultiLine covers the batch entry path: three valid
// NAME=VALUE lines, each committed by Enter (popup stays open), then
// one empty Enter to close. The export order matches input order so
// downstream consumers can rely on it for sequencing.
func TestSetVarMultiLine(t *testing.T) {
	f := loadedForm(t)
	m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)
	for _, line := range []string{"RG=rg1", "LOC=eastus", "SA=storage1"} {
		f = typeRunes(t, f, line)
		m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
		f = m.(ui.Form)
		if f.Mode() != ui.FormModeSetVar {
			t.Errorf("popup closed unexpectedly after %q; mode = %v", line, f.Mode())
		}
	}
	// Empty Enter closes.
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeList {
		t.Errorf("empty Enter should close popup; mode = %v", f.Mode())
	}
	got := f.PendingEnvExports()
	want := []string{
		"export RG='rg1'",
		"export LOC='eastus'",
		"export SA='storage1'",
	}
	if len(got) != len(want) {
		t.Fatalf("pendingExports = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("pendingExports[%d] = %q, want %q", i, got[i], w)
		}
	}
	// The flush helper returns a single newline-joined block so the CLI
	// can write it verbatim into --env-out.
	if flushed := f.FlushPendingEnvExports(); flushed != strings.Join(want, "\n")+"\n" {
		t.Errorf("FlushPendingEnvExports() = %q, want %q", flushed, strings.Join(want, "\n")+"\n")
	}
}

// TestSetVarEscDiscardsBatch locks down the cancellation semantics:
// Esc inside the popup must drop the in-progress batch. This matches
// the rest of the form's "Esc cancels and saves draft" behaviour —
// pressing Esc means "I changed my mind", and a forgotten export
// line in the shell would be worse than losing one.
func TestSetVarEscDiscardsBatch(t *testing.T) {
	f := loadedForm(t)
	m, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)
	f = typeRunes(t, f, "FOO=bar")
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if got := f.PendingEnvExports(); len(got) != 1 {
		t.Fatalf("setup pendingExports = %v, want 1 entry", got)
	}
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeList {
		t.Errorf("Esc should close popup; mode = %v", f.Mode())
	}
	if got := f.PendingEnvExports(); len(got) != 0 {
		t.Errorf("Esc must drop the pending batch; pendingExports = %v, want []", got)
	}
}

// TestSetVarFlushSurvivesDone covers the Done path: when the user
// presses Tab+Enter on the Done button, pendingExports must survive
// the form exit so the CLI can flush them to --env-out. The form
// itself doesn't write the file; it just hands the lines back.
func TestSetVarFlushSurvivesDone(t *testing.T) {
	src := ui.Sources{
		Engine: validate.NewEngine(validate.BuiltinProvider{}),
	}
	params := []metadata.Parameter{
		{Name: "--name", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--resource-group", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString},
	}
	f := ui.NewFormWithSources("x", "/tmp/out", t.TempDir(), "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: params, Summary: "."})
	f = m.(ui.Form)

	// Drive --name through the form: Enter opens edit, type a value,
	// Enter commits. Repeating for --resource-group keeps the test
	// independent of any internal field-mutation API.
	for _, name := range []string{"--name", "--resource-group"} {
		target := -1
		for i, idx := range f.Visible() {
			if f.Fields()[idx].Param.Name == name {
				target = i
				break
			}
		}
		if target < 0 {
			t.Fatalf("field %q not visible", name)
		}
		for i := 0; i < target; i++ {
			m, _ = f.Update(tea.KeyMsg{Type: tea.KeyDown})
			f = m.(ui.Form)
		}
		m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
		f = m.(ui.Form)
		if f.Mode() != ui.FormModeEdit {
			t.Fatalf("after Enter on %q: mode = %v, want FormModeEdit", name, f.Mode())
		}
		f = typeRunes(t, f, "v")
		m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
		f = m.(ui.Form)
	}

	// Open popup, queue one export.
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)
	f = typeRunes(t, f, "FOO=bar")
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter}) // empty Enter closes popup
	f = m.(ui.Form)

	// Tab → Done, Enter → confirm.
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyTab})
	f = m.(ui.Form)
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)

	if f.Result() == "" {
		t.Fatalf("Result should be set after Done; errorMsg=%q", f.ErrorMsg())
	}
	got := f.PendingEnvExports()
	want := []string{"export FOO='bar'"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("pendingExports after Done = %v, want %v", got, want)
	}
}

// TestSetVarThenPickerSeesIt locks down the "set then use" loop the user
// actually wants: g-popup commits a var, then Ctrl+G (from a field's
// edit mode) shows that var in the picker so the user can insert
// `$name` into the field. Without registerVar() surfacing the new entry
// into m.src.Vars / m.sessionVars, the picker would still be the
// widget-open snapshot and the new var wouldn't be reachable.
func TestSetVarThenPickerSeesIt(t *testing.T) {
	// Open the form with no vars in scope.
	f := loadedForm(t)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: testParams, Summary: "."})
	f = m.(ui.Form)

	// Use the g-popup to commit myName=Volodymyr Kharkov.
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)
	f = typeRunes(t, f, "myName=Volodymyr Kharkov")
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)

	// Empty Enter closes the popup.
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeList {
		t.Fatalf("after empty Enter, mode = %v, want FormModeList", f.Mode())
	}

	// Now open a field's edit mode and Ctrl+G → picker must include
	// the var we just set.
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeEdit {
		t.Fatalf("after Enter on field, mode = %v, want FormModeEdit", f.Mode())
	}
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	f = m.(ui.Form)
	if f.Mode() != ui.FormModeVarPick {
		t.Fatalf("after Ctrl+G in edit mode, mode = %v, want FormModeVarPick", f.Mode())
	}

	names := f.PickerVarNames()
	found := false
	for _, n := range names {
		if n == "myName" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("picker did not pick up just-set var; picker contents: %v", names)
	}
}
