package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
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

// valuesCacheTTL bounds how long fetched values are reused across form
// invocations. Short on purpose: the lists are the user's live resources,
// and a group created a few minutes ago should show up. The picker's
// free-text row covers anything newer.
const valuesCacheTTL = 3 * time.Minute

// fetchSpec says what to run for a field's values and how to present them.
type fetchSpec struct {
	command  string // az command line without the leading "az"
	sorted   bool   // sort choices; az returns resource lists in no useful order
	cacheDir string // "" disables the on-disk values cache
}

// runAz runs az with args; a package variable so tests can stub the process.
var runAz = func(ctx context.Context, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "az", args...)
	cmd.Env = append(cmd.Environ(), "AZURE_CORE_NO_COLOR=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return out, stderr.Bytes(), err
}

// fetchField runs `az <command> --output json`, parses the result, and
// returns the choices via FieldFetchedMsg. The command is read-only — the
// spec forbids `az` invocations that mutate state (§5.4, D6).
func fetchField(fieldIdx int, spec fetchSpec) tea.Cmd {
	return func() tea.Msg {
		if choices, ok := loadCachedValues(spec.cacheDir, spec.command, time.Now()); ok {
			return FieldFetchedMsg{FieldIdx: fieldIdx, Choices: choices}
		}
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		out, stderr, err := runAz(ctx, buildFetchArgs(spec.command)...)
		if err != nil {
			return FieldFetchedMsg{FieldIdx: fieldIdx, Err: fetchError(spec.command, stderr, err)}
		}
		choices, perr := parseFetchedValues(out)
		if perr != nil {
			return FieldFetchedMsg{FieldIdx: fieldIdx, Err: perr}
		}
		if spec.sorted {
			sort.Strings(choices)
		}
		saveCachedValues(spec.cacheDir, spec.command, choices, time.Now())
		return FieldFetchedMsg{FieldIdx: fieldIdx, Choices: choices}
	}
}

// fetchError turns a failed az run into the one-line message shown next to
// the field. The two failures every new user hits get a plain explanation
// instead of az's multi-line stderr.
func fetchError(command string, stderr []byte, err error) error {
	msg := string(stderr)
	lower := strings.ToLower(msg)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return errors.New("az not found on PATH — type a value")
	case strings.Contains(lower, "az login"):
		return errors.New("not signed in (run az login) — type a value")
	case strings.TrimSpace(msg) == "":
		msg = err.Error()
	}
	return fmt.Errorf("az %s: %s", command, oneLine(msg))
}

// implicitSource returns the values command for params whose valid values
// are the user's existing resources but whose help text carries no
// `Values from:` hint. It is deliberately narrow: listing existing names
// for a param that names a resource to be *created* would be wrong, so only
// params that always refer to something that exists are covered.
func implicitSource(command string, p metadata.Parameter) string {
	switch {
	case p.Name == "--resource-group":
		// `az group create` names its new group --name (with
		// --resource-group only as an alias), so this never lands there.
		return "az group list"
	case p.Name == "--name" && existingGroupCommands[command]:
		return "az group list"
	}
	return ""
}

// existingGroupCommands are the `az group` commands whose --name must be
// an existing resource group.
var existingGroupCommands = map[string]bool{
	"group show":   true,
	"group delete": true,
	"group update": true,
	"group wait":   true,
	"group export": true,
}

// valuesCachePath maps a fetch command to its cache file.
func valuesCachePath(cacheDir, command string) string {
	sum := sha256.Sum256([]byte(command))
	return filepath.Join(cacheDir, "values", hex.EncodeToString(sum[:8])+".json")
}

type cachedValues struct {
	Command string    `json:"command"`
	At      time.Time `json:"at"`
	Profile time.Time `json:"profile"` // azureProfile.json mtime when fetched
	Choices []string  `json:"choices"`
}

// profileStamp returns the mtime of the Azure CLI profile. `az login`,
// `az logout` and `az account set` rewrite it, and any of them can change
// which subscription (and so which resources) a plain `az group list`
// sees; cached values from before the change are not reused.
var profileStamp = func() time.Time {
	dir := os.Getenv("AZURE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return time.Time{}
		}
		dir = filepath.Join(home, ".azure")
	}
	info, err := os.Stat(filepath.Join(dir, "azureProfile.json"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC()
}

func loadCachedValues(cacheDir, command string, now time.Time) ([]string, bool) {
	if cacheDir == "" {
		return nil, false
	}
	data, err := os.ReadFile(valuesCachePath(cacheDir, command))
	if err != nil {
		return nil, false
	}
	var c cachedValues
	if json.Unmarshal(data, &c) != nil || c.Command != command || len(c.Choices) == 0 {
		return nil, false
	}
	if !c.Profile.Equal(profileStamp()) {
		return nil, false
	}
	if age := now.Sub(c.At); age < 0 || age > valuesCacheTTL {
		return nil, false
	}
	return c.Choices, true
}

// saveCachedValues is best-effort: a failed write only costs the next
// invocation an az call.
func saveCachedValues(cacheDir, command string, choices []string, now time.Time) {
	if cacheDir == "" {
		return
	}
	path := valuesCachePath(cacheDir, command)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(cachedValues{Command: command, At: now.UTC(), Profile: profileStamp(), Choices: choices})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".values-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return
	}
	if tmp.Close() != nil {
		return
	}
	_ = os.Rename(name, path)
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
	// The values must come from the subscription the command will run
	// against, not the CLI's default one.
	if sub := lookup("--subscription"); sub != "" {
		args = append(args, "--subscription", sub)
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
	return append([]string{"--subscription"}, fetchContext[strings.Join(args, " ")]...)
}

// choiceKeys are the object fields tried, in order, as an item's value.
// version / kubernetesVersion cover `aks get-versions` / `get-upgrades`.
var choiceKeys = []string{"name", "displayName", "version", "kubernetesVersion"}

// parseFetchedValues extracts the choice list from a `az ... --output json`
// response. Heuristic (spec §4.5): in an array of plain strings each string
// is a value; for an object, the first of choiceKeys that is a non-empty string, else
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
