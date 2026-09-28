package ui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/render"
	"github.com/someson/azform/internal/shell"
	"github.com/someson/azform/internal/ui"
	"github.com/someson/azform/internal/validate"
)

// A fish buffer's (…) substitutions must come back out of the form exactly
// as typed. They used to be read as literal text and single-quoted, which
// silently turned `--name (whoami)-rg` into the string "(whoami)-rg".
func TestFishBufferSubstRoundTrips(t *testing.T) {
	params := append(typedValueParams(),
		metadata.Parameter{Name: "--location", TakesValue: true, ValueKind: metadata.ValueKindString,
			Choices: []string{"westeurope", "eastus"}})
	raw, ok := shell.ParseRawSyntax(`az group show --name (whoami)-rg --resource-group (echo my rg) --location (echo eastus)`, 0, shell.Fish)
	if !ok {
		t.Fatal("ParseRawSyntax failed")
	}
	src := ui.Sources{Buffer: raw, Dialect: render.Fish, Engine: validate.NewEngine(validate.BuiltinProvider{})}
	f := ui.NewFormWithSources("group show", "/tmp/out.txt", t.TempDir(), "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: params, Summary: "."})
	f = submit(t, m.(ui.Form))
	want := "az group show --name (whoami)-rg --resource-group (echo my rg) --location (echo eastus)"
	if f.Result() != want {
		t.Errorf("Result = %q\nwant     %q (errorMsg=%q)", f.Result(), want, f.ErrorMsg())
	}
}

// Typed into a field in fish, a (…) word is shell code (var mode) and a
// value with a quote is quoted with fish's escapes, not POSIX '\”.
func TestFishTypedValues(t *testing.T) {
	src := ui.Sources{Dialect: render.Fish}
	f := ui.NewFormWithSources("group show", "/tmp/out.txt", t.TempDir(), "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: typedValueParams(), Summary: "."})
	f = m.(ui.Form)
	f = editField(t, f, "(whoami)-rg")
	if name := f.Fields()[f.FieldIndex("--name")]; name.Mode != ui.FieldModeVar {
		t.Errorf("(…) should be var mode in fish, got %s", ui.ModeName(name.Mode))
	}
	f = sendKey(t, f, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	f = editField(t, f, "it's")
	f = submit(t, f)
	want := `az group show --name (whoami)-rg --resource-group 'it\'s'`
	if f.Result() != want {
		t.Errorf("Result = %q, want %q (errorMsg=%q)", f.Result(), want, f.ErrorMsg())
	}
}
