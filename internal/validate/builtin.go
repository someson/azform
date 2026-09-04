package validate

import (
	"context"
	"strconv"
	"strings"

	"github.com/someson/azform/internal/metadata"
)

// BuiltinProvider returns the seven built-in rules from spec §9.
type BuiltinProvider struct{}

// Rules returns the built-in rule set. ctx and command are accepted for
// interface compatibility; the built-in set is static.
func (BuiltinProvider) Rules(ctx context.Context, command string) ([]Rule, error) {
	return []Rule{
		&requiredMissing{},
		&undefinedVar{},
		&escapeError{},
		&enumOutOfRange{},
		&unknownFlag{},
		&unexpectedPositional{},
		&duplicateParam{},
	}, nil
}

// requiredMissing: Required && Enabled && Value == "" → Blocking.
type requiredMissing struct{}

func (requiredMissing) ID() string { return "builtin/required-missing" }
func (requiredMissing) Check(cmd *metadata.Command, st *FormState) []Finding {
	var out []Finding
	for _, p := range cmd.Parameters {
		if !p.Required {
			continue
		}
		if !st.Enabled[p.Name] {
			continue
		}
		if st.Values[p.Name] != "" {
			continue
		}
		out = append(out, Finding{
			Param:    p.Name,
			Severity: SeverityBlocking,
			Message:  p.Name + " is required",
			RuleID:   "builtin/required-missing",
		})
	}
	return out
}

// undefinedVar: every var-mode value referencing a name not in SessionVars.
type undefinedVar struct{}

func (undefinedVar) ID() string { return "builtin/undefined-var" }
func (undefinedVar) Check(cmd *metadata.Command, st *FormState) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, name := range st.UsedVarNames {
		if seen[name] {
			continue
		}
		seen[name] = true
		if st.SessionVars[name] {
			continue
		}
		param := ""
		for pn, m := range st.Modes {
			if m != FieldModeVar {
				continue
			}
			if usesVarName(st.Values[pn], name) {
				param = pn
				break
			}
		}
		out = append(out, Finding{
			Param:    param,
			Severity: SeverityBlocking,
			Message:  "$" + name + " is not set in this shell",
			RuleID:   "builtin/undefined-var",
		})
	}
	return out
}

func usesVarName(value, name string) bool {
	// A var-mode field may carry a list of refs (`$a1 $a2`). Split on
	// whitespace and check each token — joining-then-comparing breaks for
	// any list with more than one element.
	if value == "" {
		return false
	}
	for _, tok := range strings.Fields(value) {
		if len(tok) < 2 || tok[0] != '$' || tok[1] == '(' {
			continue
		}
		rest := tok[1:]
		if rest == "" {
			continue
		}
		if rest[0] == '{' && tok[len(tok)-1] == '}' {
			rest = rest[1 : len(rest)-1]
		}
		if rest == name {
			return true
		}
	}
	return false
}

// escapeError: literal-mode value with unclosed quote or backtick.
type escapeError struct{}

func (escapeError) ID() string { return "builtin/escape-error" }
func (escapeError) Check(cmd *metadata.Command, st *FormState) []Finding {
	var out []Finding
	for name, m := range st.Modes {
		if m != FieldModeLiteral {
			continue
		}
		val := st.Values[name]
		if strings.Count(val, `"`)%2 == 1 || strings.Count(val, "`")%2 == 1 {
			out = append(out, Finding{
				Param:    name,
				Severity: SeverityBlocking,
				Message:  name + ": unclosed quote in value",
				RuleID:   "builtin/escape-error",
			})
		}
	}
	return out
}

// enumOutOfRange: enum value not in Choices (when enabled).
type enumOutOfRange struct{}

func (enumOutOfRange) ID() string { return "builtin/enum-out-of-range" }
func (enumOutOfRange) Check(cmd *metadata.Command, st *FormState) []Finding {
	var out []Finding
	for _, p := range cmd.Parameters {
		if p.ValueKind != metadata.ValueKindEnum && p.ValueKind != metadata.ValueKindBool {
			continue
		}
		if !st.Enabled[p.Name] {
			continue
		}
		// Bare bool switches (--debug, --help, --verbose, etc.) have empty
		// Choices: their "value" is the parser's structural "true" set when
		// the flag is bare-presented. There is no enum to validate against.
		if p.ValueKind == metadata.ValueKindBool && len(p.Choices) == 0 {
			continue
		}
		val := st.Values[p.Name]
		if val == "" {
			continue
		}
		ok := false
		for _, c := range p.Choices {
			if c == val {
				ok = true
				break
			}
		}
		if !ok {
			suggest := ""
			if len(p.Choices) > 0 {
				suggest = p.Choices[0]
			}
			out = append(out, Finding{
				Param:    p.Name,
				Severity: SeverityWarning,
				Message:  p.Name + ": " + val + " is not one of the allowed values",
				Suggest:  suggest,
				RuleID:   "builtin/enum-out-of-range",
			})
		}
	}
	return out
}

// unknownFlag: each flag from raw buffer not in metadata. Suggest uses
// Levenshtein to propose the nearest known flag (distance ≤ 2).
type unknownFlag struct{}

func (unknownFlag) ID() string { return "builtin/unknown-flag" }
func (unknownFlag) Check(cmd *metadata.Command, st *FormState) []Finding {
	out := make([]Finding, 0, len(st.UnknownFlags))
	for _, f := range st.UnknownFlags {
		out = append(out, Finding{
			Param:    "",
			Severity: SeverityWarning,
			Message:  "unknown flag " + f,
			Suggest:  suggestSimilar(f, cmd.Parameters),
			RuleID:   "builtin/unknown-flag",
		})
	}
	return out
}

func suggestSimilar(flag string, params []metadata.Parameter) string {
	best := ""
	bestDist := 1 << 30
	stripped := strings.TrimLeft(flag, "-")
	for _, p := range params {
		for _, n := range p.AllNames() {
			d := Levenshtein(stripped, strings.TrimLeft(n, "-"))
			if d < bestDist {
				bestDist = d
				best = n
			}
		}
	}
	if bestDist <= 2 {
		return best
	}
	return ""
}

// unexpectedPositional: each non-flag token from raw buffer.
type unexpectedPositional struct{}

func (unexpectedPositional) ID() string { return "builtin/unexpected-positional" }
func (unexpectedPositional) Check(cmd *metadata.Command, st *FormState) []Finding {
	out := make([]Finding, 0, len(st.Positional))
	for _, tok := range st.Positional {
		out = append(out, Finding{
			Param:    "",
			Severity: SeverityWarning,
			Message:  "unexpected positional: " + tok,
			RuleID:   "builtin/unexpected-positional",
		})
	}
	return out
}

// duplicateParam: flag appearing ≥2 times in raw buffer.
type duplicateParam struct{}

func (duplicateParam) ID() string { return "builtin/duplicate-param" }
func (duplicateParam) Check(cmd *metadata.Command, st *FormState) []Finding {
	var out []Finding
	for flag, n := range st.FlagOccurrences {
		if n < 2 {
			continue
		}
		out = append(out, Finding{
			Param:    flag,
			Severity: SeverityWarning,
			Message:  flag + " specified " + strconv.Itoa(n) + " times",
			RuleID:   "builtin/duplicate-param",
		})
	}
	return out
}

// Levenshtein returns the edit distance between strings a and b. Used for
// did-you-mean suggestions on unknown flags (spec §6.8).
func Levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n, m := len(ra), len(rb)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}
	dp := make([]int, m+1)
	for j := range dp {
		dp[j] = j
	}
	for i := 1; i <= n; i++ {
		prev := i
		for j := 1; j <= m; j++ {
			var curr int
			if ra[i-1] == rb[j-1] {
				curr = dp[j-1]
			} else {
				curr = 1 + min3(dp[j], prev, dp[j-1])
			}
			dp[j-1] = prev
			prev = curr
		}
		dp[m] = prev
	}
	return dp[m]
}

func min3(a, b, c int) int {
	if a > b {
		a = b
	}
	if a > c {
		a = c
	}
	return a
}
