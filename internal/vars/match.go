package vars

import (
	"strings"

	"github.com/someson/azform/internal/metadata"
)

// Match binds one shell variable to one command parameter.
type Match struct {
	ParamName string // "--resource-group"
	VarName   string // "RG"
	Value     string // resolved value, e.g. "my-group"
}

// MatchVariables maps variables to parameter slots in params. Each parameter
// is bound to at most one variable. Two signals are applied per variable:
//  1. name heuristic (spec §4.2): normalised match against param canonical
//     names and aliases, with _NAME / _LOCATION / _GROUP / _SKU suffix
//     shortcuts and an AZURE_DEFAULTS_* prefix preference (covered by
//     normalisation folding).
//  2. value signal (spec §4.2): if the variable's value is in any param's
//     Choices list, the variable is bound to that param.
//
// Sensitive variables are filtered before matching.
func MatchVariables(in []Variable, params []metadata.Parameter) []Match {
	used := make(map[string]bool, len(params))
	var out []Match

	for _, v := range in {
		if IsSensitiveName(v.Name) {
			continue
		}
		if param := matchByName(v, params); param != "" && !used[param] {
			out = append(out, Match{ParamName: param, VarName: v.Name, Value: v.Value})
			used[param] = true
			continue
		}
		if param := matchByValue(v, params); param != "" && !used[param] {
			out = append(out, Match{ParamName: param, VarName: v.Name, Value: v.Value})
			used[param] = true
		}
	}
	return out
}

// matchByName applies the §4.2 name heuristic.
func matchByName(v Variable, params []metadata.Parameter) string {
	vn := normalise(v.Name)
	if vn == "" {
		return ""
	}
	// Direct normalised match against canonical name or alias.
	for _, p := range params {
		for _, n := range p.AllNames() {
			if normalise(stripDashes(n)) == vn {
				return p.Name
			}
		}
	}
	// Explicit short-name aliases from spec §4.2 (RG, LOC, SA).
	for _, p := range params {
		for _, alias := range shortAliases[p.Name] {
			if alias == vn {
				return p.Name
			}
		}
	}
	// Suffix shortcut: variable ends in _NAME / _LOCATION / _GROUP / _SKU
	// maps to --name / --location / --resource-group / --sku. The longest
	// matching suffix wins.
	suffixMap := []struct{ suffix, param string }{
		{"name", "--name"},
		{"location", "--location"},
		{"group", "--resource-group"},
		{"sku", "--sku"},
	}
	bestLen := -1
	var bestParam string
	for _, s := range suffixMap {
		if strings.HasSuffix(vn, s.suffix) && len(s.suffix) > bestLen {
			bestLen = len(s.suffix)
			bestParam = s.param
		}
	}
	if bestParam != "" {
		for _, p := range params {
			if p.Name == bestParam {
				return bestParam
			}
		}
	}
	return ""
}

// shortAliases is the explicit short-name mapping from spec §4.2. Keys are
// canonical param names, values are the short forms after normalisation.
var shortAliases = map[string][]string{
	"--resource-group": {"rg"},
	"--location":       {"loc"},
	"--name":           {"sa"},
}

// matchByValue binds v to the first param whose Choices contains v.Value.
// Bool params are excluded: values like "true"/"false" are far too common
// (CI=true, DEBUG=false, …) to be a meaningful binding signal.
func matchByValue(v Variable, params []metadata.Parameter) string {
	for _, p := range params {
		if p.ValueKind == metadata.ValueKindBool {
			continue
		}
		for _, c := range p.Choices {
			if c == v.Value {
				return p.Name
			}
		}
	}
	return ""
}

// normalise lower-cases the name and folds '-' / '_' away.
func normalise(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r == '-' || r == '_' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func stripDashes(s string) string {
	return strings.ReplaceAll(s, "-", "")
}
