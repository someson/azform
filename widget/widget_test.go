package widget_test

import (
	"errors"
	"os"
	"testing"

	"github.com/someson/azform/widget"
)

// TestScriptMatchesSourceFiles is the anti-drift guarantee: what the
// binary emits must be byte-identical to the file the repo, the
// Makefile and the e2e tests treat as the source of truth. If these
// ever diverge, `azform shell-init` would install a widget nobody
// tests.
func TestScriptMatchesSourceFiles(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"zsh":  "widget.zsh",
		"bash": "widget.bash",
	}
	for shell, file := range cases {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			got, err := widget.Script(shell)
			if err != nil {
				t.Fatalf("Script(%q): %v", shell, err)
			}
			if string(got) != string(want) {
				t.Errorf("Script(%q) differs from %s", shell, file)
			}
		})
	}
}

// TestScriptUnknownShell pins the error path: an unsupported shell is
// a caller error, not an empty script.
func TestScriptUnknownShell(t *testing.T) {
	t.Parallel()
	for _, shell := range []string{"fish", "sh", "", "../widget.zsh"} {
		got, err := widget.Script(shell)
		if !errors.Is(err, widget.ErrUnsupportedShell) {
			t.Errorf("Script(%q): got err %v, want ErrUnsupportedShell", shell, err)
		}
		if got != nil {
			t.Errorf("Script(%q): got %d bytes, want nil", shell, len(got))
		}
	}
}

// TestShellsListsSupported keeps the usage message and the switch in
// step — the subcommand prints this list when given a bad shell.
func TestShellsListsSupported(t *testing.T) {
	t.Parallel()
	got := widget.Shells()
	if len(got) != 2 || got[0] != "bash" || got[1] != "zsh" {
		t.Errorf("Shells() = %v, want [bash zsh]", got)
	}
}
