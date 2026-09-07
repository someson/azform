package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/someson/azform/widget"
)

// TestShellInitEmitsWidget covers the happy path install.sh depends on:
// the script goes to stdout unchanged, nothing goes to stderr, exit 0.
func TestShellInitEmitsWidget(t *testing.T) {
	t.Parallel()
	for _, shell := range []string{"zsh", "bash"} {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := runShellInit([]string{shell}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
			}
			want, err := widget.Script(shell)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Errorf("stdout is not the embedded %s widget", shell)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

// TestShellInitUsageErrors pins exit code 2 and a usage message on
// stderr for every misuse, so install.sh can rely on `set -e` catching
// a typo instead of writing an empty widget file.
func TestShellInitUsageErrors(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"no shell":    {},
		"unknown":     {"fish"},
		"unsupported": {"sh"},
		"extra args":  {"zsh", "bash"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := runShellInit(args, &stdout, &stderr); code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), "shell-init") {
				t.Errorf("stderr should show usage; got %q", stderr.String())
			}
		})
	}
}

// TestRunDispatchesShellInit checks the subcommand is intercepted
// before flag parsing — `shell-init` is a bare positional, which the
// flag-based path would otherwise treat as an az command path.
func TestRunDispatchesShellInit(t *testing.T) {
	if code := run([]string{"shell-init", "zsh"}); code != 0 {
		t.Fatalf("run(shell-init zsh) = %d, want 0", code)
	}
}
