package ui_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/someson/azform/internal/shell"
	"github.com/someson/azform/internal/ui"
	"github.com/someson/azform/internal/validate"
)

// Buffer values the shell expands — command substitutions, a $VAR inside a
// longer word, ~ — used to be read as literal text: re-emitted single-quoted,
// truncated at the $( or dropped altogether. They must come back as typed,
// and a real bash must pass az the same arguments before and after the form.
func TestBufferExpansionsRoundTrip(t *testing.T) {
	cases := []struct{ in, want string }{
		{`az group show --name $(whoami) --resource-group rg`, ""},
		{`az group show --name "$(whoami)" --resource-group rg`, ""},
		{`az group show --name pre-$(whoami) --resource-group rg`, ""},
		{"az group show --name `whoami` --resource-group rg", ""},
		{`az group show --name pre-$RG --resource-group rg`, ""},
		{`az group show --name "pre-$RG" --resource-group rg`, ""},
		{`az group show --name ${RG}-x --resource-group rg`, ""},
		{`az group show --name ~/x --resource-group rg`, ""},
		{`az group show --name $(printf 'a b') --resource-group rg`, ""},
		// --flag=value is normalised to --flag value; bash sees the same args.
		{`az group show --name=$(whoami) --resource-group rg`, `az group show --name $(whoami) --resource-group rg`},
		// A literal dollar is still a literal.
		{`az group show --name 'a$b' --resource-group rg`, ""},
	}
	bash, _ := exec.LookPath("bash")
	argv := func(line string) string {
		out, err := exec.Command(bash, "-c", `RG=myrg; az() { printf '<%s>' "$@"; }; `+line).CombinedOutput()
		if err != nil {
			t.Fatalf("bash %q: %v\n%s", line, err, out)
		}
		return string(out)
	}
	for _, tc := range cases {
		want := tc.want
		if want == "" {
			want = tc.in
		}
		raw, ok := shell.ParseRaw(tc.in, 0)
		if !ok {
			t.Fatalf("ParseRaw(%q) failed", tc.in)
		}
		src := ui.Sources{Buffer: raw, Engine: validate.NewEngine(validate.BuiltinProvider{})}
		f := ui.NewFormWithSources("group show", "/tmp/out.txt", t.TempDir(), "test", nil, src)
		m, _ := f.Update(ui.MetadataLoadedMsg{Params: typedValueParams(), Summary: "."})
		f = submit(t, m.(ui.Form))
		got := f.Result()
		if got != want {
			t.Errorf("in:  %s\ngot: %s\nwant %s (errorMsg=%q)", tc.in, got, want, f.ErrorMsg())
			continue
		}
		// Compare what bash hands az; skip the normalised --flag=value case
		// (az reads both forms the same) and ~, which depends on $HOME.
		if bash != "" && tc.want == "" && !strings.Contains(tc.in, "~") {
			if a, b := argv(tc.in), argv(got); a != b {
				t.Errorf("bash passes different args:\n in  %s -> %s\n out %s -> %s", tc.in, a, got, b)
			}
		}
	}
}
