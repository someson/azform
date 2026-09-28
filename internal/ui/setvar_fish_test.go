package ui

import (
	"testing"

	"github.com/someson/azform/internal/render"
)

// TestShellVarLineForFish: fish has no NAME=VALUE assignment and no
// '\” idiom, so its env-out line is `set -g NAME value` with fish quoting.
func TestShellVarLineForFish(t *testing.T) {
	cases := []struct{ name, value, want string }{
		{"RG", "westeurope", "set -g RG westeurope"},
		{"MSG", "it's", `set -g MSG 'it\'s'`},
		{"P", `a\b c`, `set -g P 'a\\b c'`},
		{"EMPTY", "", "set -g EMPTY ''"},
	}
	for _, tc := range cases {
		if got := shellVarLineFor(render.Fish, tc.name, tc.value); got != tc.want {
			t.Errorf("shellVarLineFor(fish, %q, %q) = %q, want %q", tc.name, tc.value, got, tc.want)
		}
	}
	if got := shellVarLineFor(render.POSIX, "MSG", "it's"); got != `MSG='it'\''s'` {
		t.Errorf("POSIX line changed: %q", got)
	}
}
