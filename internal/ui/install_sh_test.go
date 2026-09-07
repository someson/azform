package ui_test

import (
	"os/exec"
	"strings"
	"testing"
)

// runInstallLib sources install.sh in library mode (AZFORM_INSTALL_LIB=1
// makes it define its functions and return before the main flow) and
// runs one expression against it, returning trimmed stdout.
func runInstallLib(t *testing.T, expr string) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", "AZFORM_INSTALL_LIB=1 . ./install.sh; "+expr)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run %q: %v", expr, err)
	}
	return strings.TrimSpace(string(out))
}

// TestInstallShWidgetSelection pins which widget each shell gets, and
// that unsupported shells get none. The bash 3 case is the bug this
// work fixes: it used to receive the zsh widget, which made every new
// bash shell print a syntax error at startup.
func TestInstallShWidgetSelection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		shell string
		major string
		want  string
	}{
		{"zsh", "zsh", "0", "widget.zsh"},
		{"bash 5", "bash", "5", "widget.bash"},
		{"bash 4", "bash", "4", "widget.bash"},
		{"bash 3", "bash", "3", ""},
		{"sh", "sh", "0", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runInstallLib(t, "azform_widget_for_shell "+tc.shell+" "+tc.major)
			if got != tc.want {
				t.Errorf("shell=%s major=%s: got %q, want %q", tc.shell, tc.major, got, tc.want)
			}
		})
	}
}

// TestInstallShUnsupportedMessage pins the exact user-facing copy. It
// is the only thing an sh/dash user ever gets from azform, so the
// wording is part of the contract, not incidental.
func TestInstallShUnsupportedMessage(t *testing.T) {
	t.Parallel()
	got := runInstallLib(t, "azform_unsupported_message sh 0")
	want := "widget not installed: your shell is sh; azform's widget supports zsh and bash 4+. Re-run from your target shell."
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestInstallShOldBashMessage covers the other unsupported branch: the
// message must name the version found and must not hardcode a package
// manager, since bash 3.x is a version problem, not a macOS problem.
func TestInstallShOldBashMessage(t *testing.T) {
	t.Parallel()
	got := runInstallLib(t, "azform_unsupported_message bash 3")
	if !strings.Contains(got, "bash 4+") {
		t.Errorf("message should state the requirement; got %q", got)
	}
	if !strings.Contains(got, "3") {
		t.Errorf("message should name the version found; got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "brew") {
		t.Errorf("message must not hardcode a package manager; got %q", got)
	}
}
