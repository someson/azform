package ui_test

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// repoRoot returns the repository root relative to this package
// (internal/ui), for tests that shell out to repo-relative paths.
func repoRoot(t *testing.T) string {
	t.Helper()
	return "../.."
}

// bashAtLeast4 returns the path to a bash >= 4, or "" when none is
// available. Candidates are probed in order because $SHELL, `bash` on
// PATH, and a package-manager bash can be three different binaries at
// three different versions — macOS in particular ships 3.2 as
// /bin/bash while Homebrew installs 5.x elsewhere.
func bashAtLeast4(t *testing.T) string {
	t.Helper()
	for _, cand := range []string{"/opt/homebrew/bin/bash", "/usr/local/bin/bash", "bash"} {
		p, err := exec.LookPath(cand)
		if err != nil {
			continue
		}
		out, err := exec.Command(p, "-c", `echo "${BASH_VERSINFO[0]}"`).Output()
		if err != nil {
			continue
		}
		// Numeric, not lexical: "10" sorts before "4" as a string.
		major, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			continue
		}
		if major >= 4 {
			return p
		}
	}
	return ""
}

// TestBashDumpVarsFiltersTypes pins the type filtering the bash widget
// needs to match what zsh's ${(k)parameters} gives for free: plain
// scalars and integers are dumped for the picker, while arrays,
// associative arrays and readonly specials (PPID and friends) are
// skipped so they never reach the variable picker as noise.
func TestBashDumpVarsFiltersTypes(t *testing.T) {
	t.Parallel()
	bash := bashAtLeast4(t)
	if bash == "" {
		t.Skip("no bash >= 4 available")
	}
	out := t.TempDir() + "/vars"
	script := `
source widget/widget.bash
SCALAR=plain
declare -i NUMBER=42
declare -a ARRAY=(a b c)
declare -A ASSOC=([k]=v)
declare -r READONLY=nope
azform_bash_dump_vars "` + out + `"
`
	cmd := exec.Command(bash, "-c", script)
	cmd.Dir = repoRoot(t)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dump failed: %v\n%s", err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	got := string(data)
	for _, want := range []string{"SCALAR=plain\x00", "NUMBER=42\x00"} {
		if !strings.Contains(got, want) {
			t.Errorf("dump missing %q", want)
		}
	}
	for _, unwanted := range []string{"ARRAY=", "ASSOC=", "READONLY=", "PPID="} {
		if strings.Contains(got, unwanted) {
			t.Errorf("dump contains %q; arrays, assoc arrays and readonly must be filtered", unwanted)
		}
	}
}
