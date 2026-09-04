package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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

// parseFetchedValues extracts the choice list from a `az ... --output json`
// response. Heuristic (spec §4.5): the array element's `name` field
// (fallback: `displayName`, then first non-empty string field) is the value.
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
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if s := stringFromMap(obj, "name"); s != "" {
			out = append(out, s)
			continue
		}
		if s := stringFromMap(obj, "displayName"); s != "" {
			out = append(out, s)
			continue
		}
		for _, val := range obj {
			if s, ok := val.(string); ok && s != "" {
				out = append(out, s)
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("az output: empty array")
	}
	return out, nil
}

// findArray descends into a decoded JSON value, returning the first array it
// finds. `az ... --output json` typically returns either an array or an
// object with one array-valued field (e.g. `{"value": [...]}`).
func findArray(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		for _, val := range x {
			if arr := findArray(val); arr != nil {
				return arr
			}
		}
	}
	return nil
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
	if len(s) > 200 {
		s = s[:197] + "..."
	}
	return s
}
