package shell_test

import (
	"testing"

	"github.com/someson/azform/internal/shell"
)

func TestTokenizeSimple(t *testing.T) {
	tokens := shell.Tokenize("az group create")
	if len(tokens) != 3 {
		t.Fatalf("got %d tokens, want 3", len(tokens))
	}
	if tokens[0].Kind != shell.TokWord || tokens[0].Value != "az" {
		t.Errorf("tokens[0] = %+v, want Word(az)", tokens[0])
	}
	if tokens[1].Value != "group" {
		t.Errorf("tokens[1].Value = %q, want \"group\"", tokens[1].Value)
	}
	if tokens[2].Value != "create" {
		t.Errorf("tokens[2].Value = %q, want \"create\"", tokens[2].Value)
	}
}

func TestTokenizeDoubleQuoted(t *testing.T) {
	tokens := shell.Tokenize(`--name "my group"`)
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2", len(tokens))
	}
	if tokens[0].Value != "--name" {
		t.Errorf("tokens[0].Value = %q, want \"--name\"", tokens[0].Value)
	}
	if tokens[1].Value != "my group" {
		t.Errorf("tokens[1].Value = %q, want \"my group\"", tokens[1].Value)
	}
	if tokens[1].Raw != `"my group"` {
		t.Errorf("tokens[1].Raw = %q, want %q", tokens[1].Raw, `"my group"`)
	}
}

func TestTokenizeSingleQuoted(t *testing.T) {
	tokens := shell.Tokenize("--tags 'env=prod owner=vladi'")
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2", len(tokens))
	}
	if tokens[1].Value != "env=prod owner=vladi" {
		t.Errorf("tokens[1].Value = %q, want \"env=prod owner=vladi\"", tokens[1].Value)
	}
	if tokens[1].Raw != "'env=prod owner=vladi'" {
		t.Errorf("tokens[1].Raw = %q, want \"'env=prod owner=vladi'\"", tokens[1].Raw)
	}
}

func TestTokenizeLineContinuation(t *testing.T) {
	tokens := shell.Tokenize("az group create \\\n  --name x")
	// line continuation drops the \ and \n; leading spaces on next line are whitespace
	values := make([]string, len(tokens))
	for i, tok := range tokens {
		values[i] = tok.Value
	}
	want := []string{"az", "group", "create", "--name", "x"}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens %v, want %v", len(tokens), values, want)
	}
	for i, w := range want {
		if tokens[i].Value != w {
			t.Errorf("tokens[%d].Value = %q, want %q", i, tokens[i].Value, w)
		}
	}
}

func TestTokenizeCmdSubstDollar(t *testing.T) {
	tokens := shell.Tokenize("RG=$(az group list)")
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2", len(tokens))
	}
	if tokens[0].Kind != shell.TokWord || tokens[0].Value != "RG=" {
		t.Errorf("tokens[0] = %+v, want Word(RG=)", tokens[0])
	}
	if tokens[1].Kind != shell.TokCmdSubst {
		t.Errorf("tokens[1].Kind = %v, want TokCmdSubst", tokens[1].Kind)
	}
	if tokens[1].Inner != "az group list" {
		t.Errorf("tokens[1].Inner = %q, want \"az group list\"", tokens[1].Inner)
	}
	if tokens[1].Raw != "$(az group list)" {
		t.Errorf("tokens[1].Raw = %q, want \"$(az group list)\"", tokens[1].Raw)
	}
}

func TestTokenizeBacktick(t *testing.T) {
	tokens := shell.Tokenize("`az group list`")
	if len(tokens) != 1 {
		t.Fatalf("got %d tokens, want 1", len(tokens))
	}
	if tokens[0].Kind != shell.TokCmdSubst {
		t.Errorf("tokens[0].Kind = %v, want TokCmdSubst", tokens[0].Kind)
	}
	if tokens[0].Inner != "az group list" {
		t.Errorf("tokens[0].Inner = %q, want \"az group list\"", tokens[0].Inner)
	}
}

func TestTokenizeOperators(t *testing.T) {
	tokens := shell.Tokenize("az list && az create")
	wantKinds := []shell.TokenKind{
		shell.TokWord, shell.TokWord, shell.TokOp, shell.TokWord, shell.TokWord,
	}
	if len(tokens) != len(wantKinds) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(wantKinds))
	}
	for i, k := range wantKinds {
		if tokens[i].Kind != k {
			t.Errorf("tokens[%d].Kind = %v, want %v", i, tokens[i].Kind, k)
		}
	}
	if tokens[2].Raw != "&&" {
		t.Errorf("operator Raw = %q, want \"&&\"", tokens[2].Raw)
	}
}

func TestTokenizeBackslashEscape(t *testing.T) {
	// Outside quotes: \x → x
	tokens := shell.Tokenize(`--name my\ group`)
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2: %v", len(tokens), tokens)
	}
	if tokens[1].Value != "my group" {
		t.Errorf("tokens[1].Value = %q, want \"my group\"", tokens[1].Value)
	}
}

func TestTokenizeUnclosed(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantVal  string
		wantRaw  string
		wantKind shell.TokenKind
	}{
		{"double-quote", `--name "unclosed`, "unclosed", `"unclosed`, shell.TokWord},
		{"single-quote", `--name 'unclosed`, "unclosed", `'unclosed`, shell.TokWord},
		{"cmd-subst-paren", `RG=$(az group list`, "$(az group list", "$(az group list", shell.TokCmdSubst},
		{"backtick", "echo `az account show", "`az account show", "`az account show", shell.TokCmdSubst},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens := shell.Tokenize(tc.line)
			last := tokens[len(tokens)-1]
			if last.Kind != tc.wantKind {
				t.Errorf("last token Kind = %v, want %v", last.Kind, tc.wantKind)
			}
			if last.Value != tc.wantVal {
				t.Errorf("Value = %q, want %q", last.Value, tc.wantVal)
			}
			if last.Raw != tc.wantRaw {
				t.Errorf("Raw = %q, want %q (must preserve opening delimiter)", last.Raw, tc.wantRaw)
			}
			if !last.Unclosed {
				t.Error("Unclosed should be true — no closing delimiter before EOL")
			}
		})
	}
}

func TestTokenizeClosedNotFlaggedUnclosed(t *testing.T) {
	// Sanity: properly-closed tokens must NOT be flagged Unclosed.
	for _, line := range []string{
		`--name "closed"`,
		`--tags 'a=b'`,
		`RG=$(az group list)`,
		"echo `az account show`",
	} {
		tokens := shell.Tokenize(line)
		for i, tok := range tokens {
			if tok.Unclosed {
				t.Errorf("Tokenize(%q) tokens[%d] Unclosed=true, want false", line, i)
			}
		}
	}
}

func TestTokenizeStartPositions(t *testing.T) {
	tokens := shell.Tokenize("az group create")
	if tokens[0].Start != 0 || tokens[0].End != 2 {
		t.Errorf("tokens[0] Start=%d End=%d, want 0..2", tokens[0].Start, tokens[0].End)
	}
	if tokens[1].Start != 3 || tokens[1].End != 8 {
		t.Errorf("tokens[1] Start=%d End=%d, want 3..8", tokens[1].Start, tokens[1].End)
	}
}

// TestTokenizeFishSingleQuotes: fish escapes \' and \\ inside single
// quotes, so `'it\'s'` is one word, where POSIX would end the quote at
// the backslash-escaped quote and split the value.
func TestTokenizeFishSingleQuotes(t *testing.T) {
	cases := []struct {
		line, want string
		words      int
	}{
		{`az x --name 'it\'s'`, "it's", 4},
		{`az x --name 'a\\b'`, `a\b`, 4},
		{`az x --name 'a\b'`, `a\b`, 4}, // other backslashes are literal
		{`az x --name 'end\\'`, `end\`, 4},
	}
	for _, tc := range cases {
		toks := shell.TokenizeSyntax(tc.line, shell.Fish)
		if len(toks) != tc.words {
			t.Errorf("%s: got %d tokens, want %d: %+v", tc.line, len(toks), tc.words, toks)
			continue
		}
		last := toks[len(toks)-1]
		if last.Value != tc.want || last.Unclosed {
			t.Errorf("%s: value = %q (unclosed=%v), want %q", tc.line, last.Value, last.Unclosed, tc.want)
		}
	}
	// POSIX behaviour is unchanged.
	toks := shell.Tokenize(`az x --name 'a\'`)
	if last := toks[len(toks)-1]; last.Value != `a\` {
		t.Errorf("POSIX single quotes must not treat backslash specially; got %q", last.Value)
	}
}

func TestParseRawSyntaxFishQuotedValue(t *testing.T) {
	raw, ok := shell.ParseRawSyntax(`az group create --name 'it\'s' --location westeurope`, 0, shell.Fish)
	if !ok {
		t.Fatal("no az segment found")
	}
	var found bool
	for _, tok := range raw.FlagTokens {
		if tok.Value == "it's" {
			found = true
		}
	}
	if !found {
		t.Errorf("flag tokens lost the fish-quoted value: %+v", raw.FlagTokens)
	}
}
