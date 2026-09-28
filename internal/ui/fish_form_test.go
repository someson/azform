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

// fish interprets unquoted escapes like \t and \x41; such a word must
// reach fish as typed, not as the letters the tokenizer sees.
func TestFishBufferEscapesRoundTrip(t *testing.T) {
	line := `az group show --name a\x41b --resource-group tab\tsep`
	raw, ok := shell.ParseRawSyntax(line, 0, shell.Fish)
	if !ok {
		t.Fatal("ParseRawSyntax failed")
	}
	src := ui.Sources{Buffer: raw, Dialect: render.Fish, Engine: validate.NewEngine(validate.BuiltinProvider{})}
	f := ui.NewFormWithSources("group show", "/tmp/out.txt", t.TempDir(), "test", nil, src)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: typedValueParams(), Summary: "."})
	f = submit(t, m.(ui.Form))
	if f.Result() != line {
		t.Errorf("Result = %q, want %q (errorMsg=%q)", f.Result(), line, f.ErrorMsg())
	}
	// Plain escapes of special characters are still literal text.
	raw, _ = shell.ParseRawSyntax(`az group show --name a\ b --resource-group rg`, 0, shell.Fish)
	pb := shell.MatchParams(raw, typedValueParams())
	if pb.Params[0].Expands || pb.Params[0].Value != "a b" {
		t.Errorf(`a\ b: value=%q expands=%v, want literal "a b"`, pb.Params[0].Value, pb.Params[0].Expands)
	}
}
