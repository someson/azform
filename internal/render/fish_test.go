package render_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/someson/azform/internal/render"
)

func TestEscapeFish(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Standard_LRS", "Standard_LRS"},
		{"my-group", "my-group"},
		{"@file.json", "@file.json"},
		{"key=value", "key=value"},
		{"", "''"},
		{"my resource", "'my resource'"},
		{"val'ue", `'val\'ue'`},
		{`a\b`, `'a\\b'`},
		{`it\'s`, `'it\\\'s'`},
		{"$RG", "'$RG'"},
		{"50%", "'50%'"},
		{"(x)", "'(x)'"},
		{"{a,b}", "'{a,b}'"},
		{"a\x01b", "'a\x01b'"},
	}
	for _, tc := range cases {
		if got := render.EscapeFish(tc.in); got != tc.want {
			t.Errorf("EscapeFish(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestEscapeFishRoundTripsThroughFish runs every escaped value through a
// real fish and requires it to come back byte-for-byte — the one property
// that matters, and the one a hand-written expectation table cannot prove.
func TestEscapeFishRoundTripsThroughFish(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not installed")
	}
	values := []string{
		"plain", "my resource", "val'ue", `a\b`, `it\'s`, `\`, `\\`, `'`, `''`,
		"$RG", "$(whoami)", "a;b", "a&&b", "a|b", "a>b", "*", "?", "[a]",
		"~/x", "%self", "#x", "{a,b}", "^x", "(x)", `"q"`, "tab\there",
		"héllo wörld", "end\\",
	}
	for _, v := range values {
		script := "printf %s " + render.EscapeFish(v)
		out, err := exec.Command(fish, "--no-config", "-c", script).Output()
		if err != nil {
			t.Errorf("fish -c %q: %v", script, err)
			continue
		}
		if string(out) != v {
			t.Errorf("EscapeFish(%q) = %s\nfish printed %q", v, render.EscapeFish(v), out)
		}
	}
}

func TestBuildFishDialect(t *testing.T) {
	got := render.Build(render.Command{
		Path:    "group create",
		Dialect: render.Fish,
		Fields: []render.FieldValue{
			{Name: "--name", Value: "it's", Enabled: true},
			{Name: "--location", Value: "$LOC", IsVar: true, Enabled: true},
		},
	})
	want := `az group create --name 'it\'s' --location $LOC`
	if got != want {
		t.Errorf("Build = %q, want %q", got, want)
	}
	if strings.Contains(got, `'\''`) {
		t.Errorf("fish output must not use the POSIX quote idiom: %q", got)
	}
}
