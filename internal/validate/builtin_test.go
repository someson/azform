package validate_test

import (
	"context"
	"testing"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/validate"
)

func params() []metadata.Parameter {
	return []metadata.Parameter{
		{Name: "--name", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--sku", Required: false, TakesValue: true, ValueKind: metadata.ValueKindEnum, Choices: []string{"Standard_LRS", "Premium_LRS"}},
		{Name: "--tags", Required: false, TakesValue: true, ValueKind: metadata.ValueKindString},
	}
}

func emptyState() *validate.FormState {
	return &validate.FormState{
		Params:          params(),
		Values:          map[string]string{},
		Modes:           map[string]validate.FieldMode{},
		Enabled:         map[string]bool{},
		SessionVars:     map[string]bool{},
		FlagOccurrences: map[string]int{},
	}
}

func runRules(t *testing.T, st *validate.FormState) []validate.Finding {
	t.Helper()
	p := validate.BuiltinProvider{}
	rules, err := p.Rules(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	cmd := &metadata.Command{Command: "x", Parameters: st.Params}
	var out []validate.Finding
	for _, r := range rules {
		out = append(out, r.Check(cmd, st)...)
	}
	return out
}

func hasRuleID(findings []validate.Finding, id string) bool {
	for _, f := range findings {
		if f.RuleID == id {
			return true
		}
	}
	return false
}

func TestRequiredMissing(t *testing.T) {
	st := emptyState()
	st.Enabled["--name"] = true
	fs := runRules(t, st)
	if !hasRuleID(fs, "builtin/required-missing") {
		t.Errorf("expected required-missing, got %+v", fs)
	}
	st.Values["--name"] = "x"
	fs = runRules(t, st)
	if hasRuleID(fs, "builtin/required-missing") {
		t.Errorf("required-missing should not fire when value present")
	}
}

func TestUndefinedVar(t *testing.T) {
	st := emptyState()
	st.SessionVars = map[string]bool{"RG": true}
	st.Values["--resource-group"] = "$RG"
	st.Modes["--resource-group"] = validate.FieldModeVar
	st.Enabled["--resource-group"] = true
	st.UsedVarNames = []string{"RG"}
	fs := runRules(t, st)
	if hasRuleID(fs, "builtin/undefined-var") {
		t.Errorf("RG in session: should not fire")
	}

	st.SessionVars = map[string]bool{}
	fs = runRules(t, st)
	if !hasRuleID(fs, "builtin/undefined-var") {
		t.Errorf("RG missing: expected undefined-var, got %+v", fs)
	}
}

func TestUndefinedVarBraced(t *testing.T) {
	st := emptyState()
	st.Values["--name"] = "${MY_NAME}"
	st.Modes["--name"] = validate.FieldModeVar
	st.Enabled["--name"] = true
	st.SessionVars = map[string]bool{"MY_NAME": false}
	st.UsedVarNames = []string{"MY_NAME"}
	fs := runRules(t, st)
	if !hasRuleID(fs, "builtin/undefined-var") {
		t.Errorf("braced var missing should fire")
	}
}

func TestCommandSubstNotUndefined(t *testing.T) {
	st := emptyState()
	st.Values["--name"] = "$(az group list)"
	st.Modes["--name"] = validate.FieldModeVar
	st.Enabled["--name"] = true
	st.UsedVarNames = []string{}
	fs := runRules(t, st)
	if hasRuleID(fs, "builtin/undefined-var") {
		t.Errorf("command substitution should not fire undefined-var")
	}
}

func TestEnumOutOfRangeSkipsSwitches(t *testing.T) {
	// Bare bool switches like --debug, --help, --verbose have empty
	// Choices: their value is the parser's structural "true" and the rule
	// must not compare it against an empty list.
	st := emptyState()
	st.Params = []metadata.Parameter{
		{Name: "--debug", TakesValue: false, ValueKind: metadata.ValueKindBool},
		{Name: "--help", TakesValue: false, ValueKind: metadata.ValueKindBool},
	}
	st.Enabled["--debug"] = true
	st.Enabled["--help"] = true
	st.Values["--debug"] = "true"
	st.Values["--help"] = "true"

	fs := runRules(t, st)
	if hasRuleID(fs, "builtin/enum-out-of-range") {
		t.Errorf("enum-out-of-range should skip bare bool switches; got %+v", fs)
	}
}

func TestUndefinedVarMultiVarList(t *testing.T) {
	// Multi-var list value (`$a1 $a2`): usesVarName must still match each
	// individual name so the undefinedVar rule can attach the finding to
	// the right param.
	st := emptyState()
	st.Values["--servers"] = "$address1 $address2"
	st.Modes["--servers"] = validate.FieldModeVar
	st.Enabled["--servers"] = true
	st.SessionVars = map[string]bool{"address1": true}
	st.UsedVarNames = []string{"address1", "address2"}

	// address2 is missing from session: must fire.
	fs := runRules(t, st)
	if !hasRuleID(fs, "builtin/undefined-var") {
		t.Fatalf("expected undefined-var for missing address2, got %+v", fs)
	}
	// Confirm the finding is attached to --servers (usesVarName match).
	var found bool
	for _, f := range fs {
		if f.RuleID == "builtin/undefined-var" && f.Param == "--servers" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected undefined-var finding on --servers, got %+v", fs)
	}

	// Both defined: should not fire.
	st.SessionVars["address2"] = true
	if hasRuleID(runRules(t, st), "builtin/undefined-var") {
		t.Errorf("both vars defined: should not fire")
	}
}

func TestEscapeError(t *testing.T) {
	st := emptyState()
	st.Values["--name"] = `unclosed "quote`
	st.Modes["--name"] = validate.FieldModeLiteral
	st.Enabled["--name"] = true
	fs := runRules(t, st)
	if !hasRuleID(fs, "builtin/escape-error") {
		t.Errorf("unclosed quote should fire")
	}
	st.Values["--name"] = `"my group"`
	fs = runRules(t, st)
	if hasRuleID(fs, "builtin/escape-error") {
		t.Errorf("closed quotes should not fire")
	}
	st.Values["--name"] = "my group"
	fs = runRules(t, st)
	if hasRuleID(fs, "builtin/escape-error") {
		t.Errorf("bare space should not fire")
	}
}

func TestEnumOutOfRange(t *testing.T) {
	st := emptyState()
	st.Enabled["--sku"] = true
	st.Values["--sku"] = "Bogus"
	fs := runRules(t, st)
	if !hasRuleID(fs, "builtin/enum-out-of-range") {
		t.Errorf("bogus enum should fire")
	}
	st.Values["--sku"] = "Standard_LRS"
	fs = runRules(t, st)
	if hasRuleID(fs, "builtin/enum-out-of-range") {
		t.Errorf("valid enum should not fire")
	}
	st.Enabled["--sku"] = false
	st.Values["--sku"] = "Bogus"
	fs = runRules(t, st)
	if hasRuleID(fs, "builtin/enum-out-of-range") {
		t.Errorf("disabled optional should not fire")
	}
}

func TestBoolOutOfRange(t *testing.T) {
	// Extend params with a bool that has a closed choice set.
	st := emptyState()
	st.Params = append(st.Params, metadata.Parameter{
		Name: "--allow-blob-public-access", TakesValue: true,
		ValueKind: metadata.ValueKindBool, Choices: []string{"false", "true"},
	})
	st.Enabled["--allow-blob-public-access"] = true
	st.Values["--allow-blob-public-access"] = "$FLAG"
	st.Modes["--allow-blob-public-access"] = validate.FieldModeVar
	fs := runRules(t, st)
	if !hasRuleID(fs, "builtin/enum-out-of-range") {
		t.Errorf("var ref on bool with closed choices should fire enum-out-of-range")
	}
	st.Values["--allow-blob-public-access"] = "true"
	st.Modes["--allow-blob-public-access"] = validate.FieldModeLiteral
	fs = runRules(t, st)
	if hasRuleID(fs, "builtin/enum-out-of-range") {
		t.Errorf("valid bool literal should not fire")
	}
}

func TestUnknownFlag(t *testing.T) {
	st := emptyState()
	st.UnknownFlags = []string{"--nam"} // close to --name
	fs := runRules(t, st)
	if !hasRuleID(fs, "builtin/unknown-flag") {
		t.Errorf("unknown flag should fire")
	}
	if fs[0].Severity != validate.SeverityWarning {
		t.Errorf("severity = %v, want Warning", fs[0].Severity)
	}
	if fs[0].Suggest != "--name" {
		t.Errorf("Suggest = %q, want \"--name\"", fs[0].Suggest)
	}
}

func TestUnexpectedPositional(t *testing.T) {
	st := emptyState()
	st.Positional = []string{"surprise"}
	fs := runRules(t, st)
	if !hasRuleID(fs, "builtin/unexpected-positional") {
		t.Errorf("positional should fire")
	}
}

func TestDuplicateParam(t *testing.T) {
	st := emptyState()
	st.FlagOccurrences = map[string]int{"--name": 2}
	fs := runRules(t, st)
	if !hasRuleID(fs, "builtin/duplicate-param") {
		t.Errorf("duplicate should fire")
	}
	st.FlagOccurrences = map[string]int{"--name": 1}
	fs = runRules(t, st)
	if hasRuleID(fs, "builtin/duplicate-param") {
		t.Errorf("single occurrence should not fire")
	}
}

func TestBuiltinProviderRulesHaveIDs(t *testing.T) {
	p := validate.BuiltinProvider{}
	rules, err := p.Rules(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if r.ID() == "" {
			t.Errorf("rule has empty ID")
		}
		if seen[r.ID()] {
			t.Errorf("duplicate rule ID: %s", r.ID())
		}
		seen[r.ID()] = true
	}
}

func TestLevenshteinValidate(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
	}
	for _, tc := range cases {
		if got := validate.Levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("Levenshtein(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
