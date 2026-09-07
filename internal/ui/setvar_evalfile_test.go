package ui_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/ui"
	"github.com/someson/azform/internal/validate"
)

// TestSetVarFlushWritesEvalableFile locks down the round-trip the widget
// relies on: after g-popup commits, the file azform would write to
// --env-out must contain a line that, when eval'd by the widget, sets
// the var in the calling shell. We write the file here (mirroring
// what cmd/azform/main.go does on Done) and assert its content matches
// the form's pendingExports, then run `zsh -c` on it and confirm the
// var landed.
//
// Each line must be `NAME='value'` (no `export `) so the widget's eval
// loop turns it into a shell-local parameter — visible to the next
// `az …` via `$VAR` expansion but not pushed into the env. A leading
// `export` would change that semantic; this test would catch a
// regression of shellVarLine back to shellExportLine.
func TestSetVarFlushWritesEvalableFile(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not on PATH; file-content assertions below still run")
	}

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

	// Open popup, queue two exports (one with single-quote in value).
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	f = m.(ui.Form)
	f = typeRunes(t, f, "newVar=value1")
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	f = typeRunes(t, f, `TOKEN=it's`)
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter}) // empty Enter closes popup
	f = m.(ui.Form)

	// Tab → Done, Enter → confirm.
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.(ui.Form).Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(ui.Form)
	if f.Result() == "" {
		t.Fatalf("Result should be set after Done; errorMsg=%q", f.ErrorMsg())
	}

	// Write the env-out file the way main.go does on Done.
	envPath := t.TempDir() + "/env-out"
	if err := os.WriteFile(envPath, []byte(f.FlushPendingEnvExports()), 0o600); err != nil {
		t.Fatal(err)
	}

	// File contents must match the exact form expected by the widget's
	// eval loop: each line a self-contained shell assignment with no
	// leading `export `.
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	wantLines := []string{
		"newVar='value1'",
		`TOKEN='it'\''s'`,
	}
	gotLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("env-out lines = %v, want %v", gotLines, wantLines)
	}
	for i, w := range wantLines {
		if gotLines[i] != w {
			t.Errorf("env-out line %d = %q, want %q", i, gotLines[i], w)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(string(data)), "export ") {
		t.Errorf("env-out must not start with 'export ' (shell-local, not env): %q", string(data))
	}

	// Round-trip: feed the file to a fresh zsh and assert the vars
	// land. This catches any quoting regression that would make eval
	// fail or produce the wrong value.
	script := `set -e
while IFS= read -r line; do eval "$line"; done < "` + envPath + `"
print -r -- "newVar=$newVar"
print -r -- "TOKEN=$TOKEN"`
	out, err := exec.Command("zsh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("widget-side eval failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "newVar=value1") {
		t.Errorf("after eval, $newVar not visible in shell; output: %s", out)
	}
	if !strings.Contains(string(out), `TOKEN=it's`) {
		t.Errorf("after eval, $TOKEN not visible (single-quote escape broken); output: %s", out)
	}
}