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

func TestBuildFishRewritesBracedVars(t *testing.T) {
	got := render.Build(render.Command{
		Path:    "group create",
		Dialect: render.Fish,
		Fields: []render.FieldValue{
			{Name: "--name", Value: "${RG}", IsVar: true, Enabled: true},
			{Name: "--tags", Value: "${A} ${B_2}", IsVar: true, Enabled: true},
			{Name: "--location", Value: "(echo x)", IsVar: true, Enabled: true},
		},
	})
	want := "az group create --name {$RG} --tags {$A} {$B_2} --location (echo x)"
	if got != want {
		t.Errorf("Build = %q, want %q", got, want)
	}
	// POSIX output keeps ${NAME}.
	posix := render.Build(render.Command{Path: "x", Fields: []render.FieldValue{{Name: "--n", Value: "${RG}", IsVar: true, Enabled: true}}})
	if posix != "az x --n ${RG}" {
		t.Errorf("POSIX changed: %q", posix)
	}
}

// TestFishVarRefsRunInFish proves the rewritten reference expands in a
// real fish, where ${RG} would be a syntax error.
func TestFishVarRefsRunInFish(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not installed")
	}
	out, err := exec.Command(fish, "--no-config", "-c", "set RG my-rg; printf %s "+render.FishVarRefs("${RG}")).Output()
	if err != nil || string(out) != "my-rg" {
		t.Errorf("fish printed %q, err %v", out, err)
	}
}
