package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fetchTimeout caps the subprocess invocation. 30 s matches the spec's 10 s
// "offer to cancel" plus comfortable slack before the subprocess is forcibly
// killed.
const fetchTimeout = 30 * time.Second

// FieldFetchedMsg is dispatched when a lazy field fetch completes (spec §6.1
// Field fetch state). Choices is empty when Err is non-nil.
type FieldFetchedMsg struct {
	FieldIdx int
	Choices  []string
	Err      error
}

// fetchField runs `az <valuesFrom> --output json`, parses the result, and
// returns the choices via FieldFetchedMsg. The command is read-only — the
// spec forbids `az` invocations that mutate state (§5.4, D6).
func fetchField(fieldIdx int, valuesFrom string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		args := buildFetchArgs(valuesFrom)
		cmd := exec.CommandContext(ctx, "az", args...)
		cmd.Env = append(cmd.Environ(), "AZURE_CORE_NO_COLOR=1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return FieldFetchedMsg{
				FieldIdx: fieldIdx,
				Err:      fmt.Errorf("az %s: %s", valuesFrom, oneLine(stderr.String())),
			}
		}
		choices, perr := parseFetchedValues(out)
		if perr != nil {
			return FieldFetchedMsg{
				FieldIdx: fieldIdx,
				Err:      perr,
			}
		}
		return FieldFetchedMsg{
			FieldIdx: fieldIdx,
			Choices:  choices,
		}
	}
}

// buildFetchArgs splits a `ValuesFrom` hint into shell-style fields, drops
// the leading `az` token (we re-add it via exec.Command), and appends
// `--output json` if the caller did not already specify one.
func buildFetchArgs(valuesFrom string) []string {
	args := strings.Fields(valuesFrom)
	if len(args) > 0 && args[0] == "az" {
		args = args[1:]
	}
	hasOutput := false
	for _, a := range args {
		if a == "--output" || a == "-o" {
			hasOutput = true
			break
		}
		if strings.HasPrefix(a, "--output=") || strings.HasPrefix(a, "-o=") {
			hasOutput = true
			break
		}
	}
	if !hasOutput {
		args = append(args, "--output", "json")
	}
	return args
}

// fetchContext names the form params whose values some `Values from:`
// commands cannot run without. az prints the bare command in the help text
// (`az vm list-sizes`), which on its own always fails with a missing
// --location; the form passes the user's current values along instead.
var fetchContext = map[string][]string{
	"vm list-sizes":    {"--location"},
	"vm list-skus":     {"--location"},
	"aks get-versions": {"--location"},
	"aks get-upgrades": {"--resource-group", "--name"},
}

// fetchCommand turns a `Values from:` hint into the az command line to run
// (without the leading az). Hints may list alternatives
// ("az vm image list, az vm image show, …") — only the first is used — and
// carry sentence punctuation. lookup returns a param's current resolved
// value; ok is false when a required context param is still empty, in
// which case nothing should be run yet.
func fetchCommand(valuesFrom string, lookup func(param string) string) (cmd string, ok bool) {
	first, _, _ := strings.Cut(valuesFrom, ",")
	first = strings.TrimSpace(strings.Trim(strings.TrimSpace(first), "`."))
	args := strings.Fields(first)
	if len(args) > 0 && args[0] == "az" {
		args = args[1:]
	}
	if len(args) == 0 {
		return "", false
	}
	for _, a := range args {
		// Placeholders such as <name> cannot be run.
		if strings.ContainsAny(a, "<>") {
			return "", false
		}
	}
	for path, needs := range fetchContext {
		if strings.Join(args, " ") != path {
			continue
		}
		for _, param := range needs {
			v := lookup(param)
			if v == "" {
				return "", false
			}
			args = append(args, param, v)
		}
	}
	return strings.Join(args, " "), true
}

// fetchContextParams returns the params that the fetch for valuesFrom
// depends on, so their edits can invalidate already-fetched choices.
func fetchContextParams(valuesFrom string) []string {
	first, _, _ := strings.Cut(valuesFrom, ",")
	args := strings.Fields(strings.Trim(strings.TrimSpace(first), "`."))
	if len(args) > 0 && args[0] == "az" {
		args = args[1:]
	}
	return fetchContext[strings.Join(args, " ")]
}

// choiceKeys are the object fields tried, in order, as an item's value.
// version / kubernetesVersion cover `aks get-versions` / `get-upgrades`.
var choiceKeys = []string{"name", "displayName", "version", "kubernetesVersion"}

// parseFetchedValues extracts the choice list from a `az ... --output json`
// response. Heuristic (spec §4.5): in an array of plain strings each string
// is a value;
// for an object, the first of choiceKeys that is a non-empty string, else
// the first non-empty string field in key order. Duplicates are dropped.
func parseFetchedValues(raw []byte) ([]string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("parse az output: %w", err)
	}
	arr := findArray(v)
	if arr == nil {
		return nil, fmt.Errorf("az output: no array found")
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	// Strings count only in an array of plain strings; in a mixed array
	// they are noise next to the objects that describe resources.
	hasObjects := false
	for _, item := range arr {
		if _, ok := item.(map[string]any); ok {
			hasObjects = true
			break
		}
	}
	for _, item := range arr {
		switch x := item.(type) {
		case string:
			if !hasObjects {
				add(x)
			}
		case map[string]any:
			add(objectChoice(x))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("az output: empty array")
	}
	return out, nil
}

func objectChoice(obj map[string]any) string {
	for _, k := range choiceKeys {
		if s := stringFromMap(obj, k); s != "" {
			return s
		}
	}
	for _, k := range sortedKeys(obj) {
		if s, ok := obj[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// findArray descends into a decoded JSON value, returning the first array it
// finds. `az ... --output json` typically returns either an array or an
// object with one array-valued field (e.g. `{"value": [...]}`). Object keys
// are visited in sorted order so the result does not depend on Go's
// randomised map iteration.
func findArray(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		for _, k := range sortedKeys(x) {
			if arr := findArray(x[k]); arr != nil {
				return arr
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:197]) + "..."
	}
	return s
}
