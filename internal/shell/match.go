package shell

import (
	"strings"

	"github.com/someson/azform/internal/metadata"
)

// ParsedParam is one flag extracted from the shell buffer and matched against
// command metadata.
type ParsedParam struct {
	Flag     string   // canonical long form (e.g. "--resource-group"); for unknown = as typed
	RawFlag  string   // exactly as typed in the buffer (e.g. "-g", "--resource-group")
	Value    string   // unescaped value; "true" for bare bool flags
	RawValue string   // raw token text as in buffer (preserves quotes); "" for bool flags
	IsVar    bool     // true when value is a shell variable reference
	VarName  string   // variable name (e.g. "RG" from "$RG"); non-empty when IsVar and there is exactly one var
	VarNames []string // for list-kind params: variable names referenced by each consumed token (e.g. ["a1","a2"] from `"$a1" "$a2"`); empty when not a multi-var list
	Unknown  bool     // true when the flag is not found in params
	Explicit bool     // true when a value was provided (inline `--flag=…` or a value token), even if empty
}

// ParsedBuffer is the result of matching RawBuffer flag tokens against
// command metadata parameters.
type ParsedBuffer struct {
	Command     string
	Params      []ParsedParam
	Positional  []Token // unconsumed flag tokens — not used as flag name or value
	Prefix      string
	Suffix      string
	Inline      bool
	CursorParam int // index into Params where cursor falls; -1 if cursor is outside all params
}

// MatchParams maps the raw flag tokens in raw against the provided parameter
// metadata, canonicalising short aliases and detecting variable references.
// Parameters not in metadata are retained verbatim with Unknown==true.
func MatchParams(raw RawBuffer, params []metadata.Parameter) ParsedBuffer {
	pb := ParsedBuffer{
		Command:     raw.CommandPath,
		Prefix:      raw.Prefix,
		Suffix:      raw.Suffix,
		Inline:      raw.Inline,
		CursorParam: -1,
	}

	tokens := raw.FlagTokens
	// consumed[i] marks tokens that have been absorbed into some ParsedParam
	// (either as a flag name or as one of its value tokens). Anything not
	// marked after the loop is a positional — a bare word the parser left
	// over, e.g. `az storage create mystorage` where `mystorage` doesn't
	// follow any flag. This is what feeds the unexpectedPositional rule;
	// without it the rule misclassifies every flag value as a positional.
	consumed := make([]bool, len(tokens))
	i := 0
	for i < len(tokens) {
		tok := tokens[i]

		// Only process word tokens
		if tok.Kind != TokWord {
			i++
			continue
		}

		flagRaw := tok.Value

		// End of options
		if flagRaw == "--" {
			i++
			continue
		}

		// Must look like a flag — if not, this token is a stray word the
		// parser doesn't know what to do with. Leave consumed[i] = false
		// so the loop below surfaces it as a positional.
		if !strings.HasPrefix(flagRaw, "-") {
			i++
			continue
		}
		consumed[i] = true

		// Split --flag=value
		eqIdx := -1
		if strings.HasPrefix(flagRaw, "--") {
			eqIdx = strings.IndexByte(flagRaw, '=')
		}

		var flagName, inlineValue, inlineRawValue string
		if eqIdx >= 0 {
			flagName = flagRaw[:eqIdx]
			inlineValue = flagRaw[eqIdx+1:]
			inlineRawValue = inlineValue // no separate raw token
		} else {
			flagName = flagRaw
		}

		// Look up in metadata
		param := findParamByName(params, flagName)

		pp := ParsedParam{
			RawFlag: tok.Raw,
			Unknown: param == nil,
		}
		if param != nil {
			pp.Flag = param.Name
		} else {
			pp.Flag = flagName
		}

		// valueTokens holds the value tokens consumed for this flag, in
		// order. Scalar params consume at most one; list-kind params may
		// consume many. We retain the individual tokens so we can later
		// classify each as a var ref — this is what lets
		// `--servers "$a1" "$a2"` be recognised as multi-var instead of a
		// literal string.
		var valueTokens []Token

		// Determine value
		if eqIdx >= 0 {
			// --flag=value form (also captures explicit-empty --flag=)
			pp.Value = inlineValue
			pp.RawValue = inlineRawValue
			pp.Explicit = true
		} else if param != nil && !param.TakesValue {
			// Bool flag: bare, no value token
			pp.Value = "true"
			pp.RawValue = ""
		} else {
			// Look ahead for value token. Reject the next token if it looks
			// like another flag, UNLESS it looks like a negative number
			// (e.g. `--priority -1`), which az CLI accepts as a bare value.
			if i+1 < len(tokens) && tokens[i+1].Kind == TokWord {
				next := tokens[i+1].Value
				if !strings.HasPrefix(next, "-") || looksLikeNumber(next) {
					i++
					valueTokens = append(valueTokens, tokens[i])
					consumed[i] = true
					pp.Explicit = true
				}
			}
			// List-style params (space-separated values, e.g.
			// `--servers "$a1" "$a2"`) consume further bare tokens until the
			// next flag or operator. The tokens are joined with a space so
			// the form holds the full list; for an all-var list the rebuild
			// leaves the value unquoted so bash word-splits it back into the
			// original N arguments.
			if pp.Explicit && param != nil && isListKind(param.ValueKind) {
				for i+1 < len(tokens) && tokens[i+1].Kind == TokWord {
					next := tokens[i+1].Value
					if strings.HasPrefix(next, "-") && !looksLikeNumber(next) {
						break
					}
					i++
					valueTokens = append(valueTokens, tokens[i])
					consumed[i] = true
				}
			}
			if len(valueTokens) > 0 {
				values := make([]string, 0, len(valueTokens))
				raws := make([]string, 0, len(valueTokens))
				for _, t := range valueTokens {
					values = append(values, t.Value)
					raws = append(raws, t.Raw)
				}
				pp.Value = strings.Join(values, " ")
				pp.RawValue = strings.Join(raws, " ")
			}
		}

		// Detect shell variable references.
		// For a single value token, detectVarRef handles every form
		// (`$a`, `${a}`, `"$a"`). For multi-var lists, the joined value is
		// `$a1 $a2` which fails that check; classify each value token
		// separately so the whole list ends up in var mode.
		pp.IsVar, pp.VarName = detectVarRef(pp.Value)
		if param != nil && isListKind(param.ValueKind) && len(valueTokens) > 1 {
			names := make([]string, 0, len(valueTokens))
			allVars := true
			for _, t := range valueTokens {
				isV, n := detectVarRef(t.Value)
				if !isV {
					allVars = false
					break
				}
				names = append(names, n)
			}
			if allVars {
				pp.IsVar = true
				pp.VarName = ""
				pp.VarNames = names
			}
		}

		// Cursor-to-param mapping (top-level only; inline segments have CursorByte==-1).
		// tok is the flag token. If a value token was consumed (inlineValue=="" and
		// RawValue!=""), i has been incremented and tokens[i] is the value token.
		if raw.CursorByte >= 0 {
			if raw.CursorByte >= tok.Start && raw.CursorByte < tok.End {
				pb.CursorParam = len(pb.Params)
			} else if inlineValue == "" && pp.RawValue != "" {
				valTok := tokens[i]
				if raw.CursorByte >= valTok.Start && raw.CursorByte < valTok.End {
					pb.CursorParam = len(pb.Params)
				}
			}
		}

		pb.Params = append(pb.Params, pp)
		i++
	}

	// Anything still unconsumed is a positional. Word tokens only — non-word
	// tokens (operators, command substitutions) are part of the shell
	// structure, not positional arguments.
	for idx, t := range tokens {
		if !consumed[idx] && t.Kind == TokWord {
			pb.Positional = append(pb.Positional, t)
		}
	}

	return pb
}

// findParamByName returns the parameter matching flag by canonical name or alias.
func findParamByName(params []metadata.Parameter, flag string) *metadata.Parameter {
	for i := range params {
		p := &params[i]
		for _, name := range p.AllNames() {
			if name == flag {
				return p
			}
		}
	}
	return nil
}

// isListKind reports whether the value kind takes multiple space-separated
// values on the command line.
func isListKind(k metadata.ValueKind) bool {
	return k == metadata.ValueKindList || k == metadata.ValueKindKeyValue
}

// detectVarRef reports whether value is a shell variable reference and
// returns the variable name. Recognised forms: $NAME, ${NAME}.
// $(...) is command substitution and is not treated as a var ref.
func detectVarRef(value string) (bool, string) {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		name := value[2 : len(value)-1]
		if isVarName(name) {
			return true, name
		}
	}
	if strings.HasPrefix(value, "$") && len(value) > 1 && value[1] != '(' {
		name := value[1:]
		if isVarName(name) {
			return true, name
		}
	}
	return false, ""
}

// looksLikeNumber reports whether s parses as an optionally-signed decimal
// integer or float — used to distinguish `--priority -1` (numeric value) from
// `--flag1 --flag2` (missing value). Rejects "-" and "--" alone.
func looksLikeNumber(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '-' || s[0] == '+' {
		i++
	}
	if i == len(s) {
		return false
	}
	sawDigit := false
	sawDot := false
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			sawDigit = true
		case c == '.' && !sawDot:
			sawDot = true
		default:
			return false
		}
	}
	return sawDigit
}

func isVarName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// Levenshtein returns the edit distance between strings a and b.
// Used to suggest the nearest known flag when an unknown flag is encountered.
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
				curr = 1 + minInt(dp[j], minInt(prev, dp[j-1]))
			}
			dp[j-1] = prev
			prev = curr
		}
		dp[m] = prev
	}
	return dp[m]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
