package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/shell"
	"github.com/someson/azform/internal/state"
	"github.com/someson/azform/internal/vars"
)

func (m Form) handleMetadataLoaded(msg MetadataLoadedMsg) (tea.Model, tea.Cmd) {
	m.loadState = LoadStateLoaded
	m.summary = msg.Summary
	if msg.Stale {
		m.staleWarn = "↻ metadata may be outdated"
	}

	fields := make([]Field, 0, len(msg.Params))
	var reqIdx []int
	for _, p := range msg.Params {
		f := Field{
			Param:   p,
			Enabled: p.Required,
		}
		if p.Default != nil {
			f.Value = *p.Default
			f.Source = FieldSourceDefault
			f.Enabled = true
		}
		if p.Required {
			reqIdx = append(reqIdx, len(fields))
		}
		fields = append(fields, f)
	}
	m.fields = fields
	m.reqIndices = reqIdx

	// Spec §8.5 priority order (higher first; each stage only fills fields
	// still at FieldSourceNone, so higher-priority sources win):
	//   1. Parsed buffer (spec 6.8).
	//   2. Preset (M6, not implemented).
	//   3. Draft (spec 6.7) — user's last-in-progress state, applied before
	//      env/azure so an Esc'd session is restored even when $LOGNAME etc.
	//      would otherwise overwrite it.
	//   4. Remembered binding.
	//   5. Env heuristic (spec 4.2).
	//   6. Azure defaults (spec 4.4).
	//   7. Metadata default — already applied above.
	m.sessionVars = m.sessionVarNames()
	m.applyBufferPreFill(msg.Params)
	m.applyDraftRestore()
	m.applyRememberedPreFill(msg.Params)
	m.applyEnvPreFill(msg.Params)
	m.applyAzurePreFill()

	m.recomputeFindings(msg.Params)

	m.rebuildVisible()
	m.updateLayout()
	m.logFieldSources()
	// Kick off a lazy fetch for the initially focused field (spec §6.1).
	if idx := m.fieldAt(m.cursor); idx >= 0 {
		if cmd := m.maybeFetchField(idx); cmd != nil {
			return m, cmd
		}
	}
	return m, nil
}

// logFieldSources emits one debug event per field that has a non-zero
// source. Only param names and source identifiers are logged — field values
// are deliberately omitted (spec §15.3, last paragraph).
func (m *Form) logFieldSources() {
	if m.src.Debug == nil {
		return
	}
	for _, f := range m.fields {
		if f.Source == FieldSourceNone {
			continue
		}
		m.src.Debug.Event("field_source", map[string]any{
			"param":  f.Param.Name,
			"source": f.Source.Name(),
			"mode":   ModeName(f.Mode),
		})
	}
}

// applyBufferPreFill consumes parsed shell-buffer flag tokens (priority 1).
// Returns true if any field was filled.
func (m *Form) applyBufferPreFill(params []metadata.Parameter) bool {
	if len(m.src.Buffer.FlagTokens) == 0 {
		return false
	}
	parsed := shell.MatchParams(m.src.Buffer, params)
	for _, pp := range parsed.Params {
		for i := range m.fields {
			if m.fields[i].Param.Name == pp.Flag {
				f := &m.fields[i]
				value, mode := pp.Value, FieldModeLiteral
				if pp.IsVar {
					mode = FieldModeVar
					// A var ref is only valid for a closed choice set when the
					// var is defined and resolves to an allowed value; then we
					// keep the resolved literal instead of emitting "$VAR".
					// For multi-var lists (--servers "$a1" "$a2") every
					// referenced var must resolve to an allowed value; the
					// VarValue column shows the joined resolved values so the
					// user can confirm what az will receive.
					if !valueAllowedForParam(f.Param, pp.Value) {
						if resolved := resolveBufferVars(pp, m.src.Vars); resolved != "" && valueAllowedForParam(f.Param, resolved) {
							value, mode = resolved, FieldModeLiteral
						} else {
							// Reject: leave the field untouched for manual pick.
							break
						}
					}
				}
				f.Value = value
				if mode == FieldModeVar {
					// Only set VarValue when the var ref actually resolves — otherwise
					// DisplayValue renders "$VAR → $VAR" redundantly. Leaving VarValue
					// empty drops the suffix; the StatusOf grey/yellow still tells the
					// user the var isn't in this shell.
					if resolved := resolveBufferVars(pp, m.src.Vars); resolved != "" {
						f.VarValue = resolved
					}
				} else {
					f.VarValue = value
				}
				f.Mode = mode
				f.Enabled = true
				f.Source = FieldSourceBuffer
				break
			}
		}
	}
	// Position cursor on the param the user's cursor was in.
	if parsed.CursorParam >= 0 && parsed.CursorParam < len(parsed.Params) {
		targetFlag := parsed.Params[parsed.CursorParam].Flag
		m.rebuildVisible()
		for vi, fi := range m.visible {
			if m.fields[fi].Param.Name == targetFlag {
				m.cursor = vi
				break
			}
		}
	}
	return true
}

// applyEnvPreFill consumes vars.MatchVariables results (priority 5).
// Returns true if any field was filled. Skips fields already filled by the
// buffer (priority 1).
func (m *Form) applyEnvPreFill(params []metadata.Parameter) bool {
	if len(m.src.Vars) == 0 {
		return false
	}
	matches := vars.MatchVariables(m.src.Vars, params)
	if len(matches) == 0 {
		return false
	}
	filled := false
	for _, mt := range matches {
		for i := range m.fields {
			if m.fields[i].Param.Name == mt.ParamName && m.fields[i].Source == FieldSourceNone {
				// Closed choice sets (enum/bool) never take var mode: emit the
				// resolved value as a literal if allowed, else skip the match.
				if !valueAllowedForParam(m.fields[i].Param, mt.Value) {
					continue
				}
				if len(m.fields[i].Param.Choices) > 0 {
					m.fields[i].Value = mt.Value
					m.fields[i].VarValue = mt.Value
					m.fields[i].Mode = FieldModeLiteral
				} else {
					m.fields[i].Value = "$" + mt.VarName
					m.fields[i].VarValue = mt.Value
					m.fields[i].Mode = FieldModeVar
				}
				m.fields[i].Enabled = true
				m.fields[i].Source = FieldSourceEnv
				filled = true
				break
			}
		}
	}
	return filled
}

// applyAzurePreFill consumes Azure defaults (priority 6). Vars with the same
// name as an existing param are pre-filled as literals. Returns true if any
// field was filled.
func (m *Form) applyAzurePreFill() bool {
	if len(m.src.AzureDefaults) == 0 {
		return false
	}
	filled := false
	for _, av := range m.src.AzureDefaults {
		target := azureKeyToParam(av.Name)
		if target == "" {
			continue
		}
		for i := range m.fields {
			if m.fields[i].Source != FieldSourceNone {
				continue
			}
			if m.fields[i].Param.Name == target {
				m.fields[i].Value = av.Value
				m.fields[i].Mode = FieldModeLiteral
				m.fields[i].Enabled = true
				m.fields[i].Source = FieldSourceAzure
				filled = true
				break
			}
		}
	}
	return filled
}

// azureKeyToParam maps Azure CLI config keys (lowercased, underscores) to
// canonical azform parameter names. Keys not in the map are ignored.
var azureKeyToParamTable = map[string]string{
	"group":    "--resource-group",
	"location": "--location",
	"vm-size":  "--vm-size",
	"sku":      "--sku",
	"acr":      "--acr",
	"aks":      "--aks",
}

func azureKeyToParam(name string) string {
	normalised := normaliseAzureKey(name)
	if p, ok := azureKeyToParamTable[normalised]; ok {
		return p
	}
	// Fallback: try the canonical form directly (handles --foo / foo / foo_bar).
	if len(name) > 2 && name[:2] == "--" {
		return name
	}
	return "--" + normalised
}

// normaliseAzureKey turns "GROUP" or "resource_group" into "resource-group"
// for matching against canonical param names.
func normaliseAzureKey(name string) string {
	out := make([]byte, 0, len(name)+1)
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '_' {
			c = '-'
		} else if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

// applyDraftRestore restores the saved draft for this command (priority 3).
// Only fields still at FieldSourceNone are restored. Fields the user
// explicitly toggled off before cancelling come back Enabled=false so
// a binding-applied value the user removed stays removed across
// reopen cycles.
func (m *Form) applyDraftRestore() {
	saved, disabled, ok, _ := m.draftStore.Load(m.command)
	if !ok {
		return
	}
	for k, v := range saved {
		for i := range m.fields {
			if m.fields[i].Param.Name == k && m.fields[i].Source == FieldSourceNone {
				m.fields[i].Value = v
				m.fields[i].Source = FieldSourceDraft
				if v != "" {
					m.fields[i].Enabled = true
				}
				if disabled[k] {
					// User explicitly toggled this off before cancelling —
					// honour that on reopen instead of forcing it back on.
					m.fields[i].Enabled = false
				}
			}
		}
	}
	m.draftRestored = true
}

// applyRememberedPreFill consumes bindings memory (priority 4, spec §8.5).
// Returns true if any field was filled. A binding is applied only if the
// remembered var name is in the current shell session; non-enum literals
// stored in bindings (Name == "", Value != "") are applied as literals.
// Candidates are ranked by Uses with LastUsed decay; the best eligible one wins.
func (m *Form) applyRememberedPreFill(params []metadata.Parameter) bool {
	if m.src.Bindings == nil {
		return false
	}
	bindings, err := m.src.Bindings.Load()
	if err != nil || len(bindings) == 0 {
		return false
	}
	now := time.Now().UTC()
	filled := false
	for i := range m.fields {
		f := &m.fields[i]
		if f.Param.Name == "" || f.Source != FieldSourceNone {
			continue
		}
		key := state.BindingKey(m.command, f.Param.Name)
		cands := bindings[key]
		if len(cands) == 0 {
			continue
		}
		for _, c := range state.RankCandidates(cands, now) {
			if c.Name != "" && !m.sessionVars[c.Name] {
				continue
			}
			if c.Name != "" {
				// Closed choice sets (enum/bool) never take var mode: use the
				// remembered resolved value as a literal if allowed.
				if len(f.Param.Choices) > 0 {
					if !valueAllowedForParam(f.Param, c.Value) {
						continue
					}
					f.Value = c.Value
					f.VarValue = c.Value
					f.Mode = FieldModeLiteral
				} else {
					f.Value = "$" + c.Name
					f.VarValue = c.Value
					f.Mode = FieldModeVar
				}
			} else if c.Value != "" {
				if !valueAllowedForParam(f.Param, c.Value) {
					continue
				}
				f.Value = c.Value
				f.Mode = FieldModeLiteral
			} else {
				continue
			}
			f.Enabled = true
			f.Source = FieldSourceRemembered
			filled = true
			break
		}
	}
	return filled
}

// valueAllowedForParam reports whether value may populate p without breaking
// a closed choice set. Params with a known closed set (enum, or bool with
// bool-synonym choices) accept only listed values; anything else — including
// "$VAR" references picked up from the buffer, env, or bindings — is rejected
// so the form never emits e.g. `--allow-blob-public-access $FOO` where az
// expects true|false. Params with no closed set accept any value.
func valueAllowedForParam(p metadata.Parameter, value string) bool {
	if len(p.Choices) == 0 {
		return true
	}
	if p.ValueKind != metadata.ValueKindEnum && p.ValueKind != metadata.ValueKindBool {
		return true
	}
	for _, c := range p.Choices {
		if c == value {
			return true
		}
	}
	return false
}

// resolveBufferVars returns the joined resolved values for a var-mode buffer
// param. For single-var refs (pp.VarName set) this returns the matching var's
// value. For multi-var lists (pp.VarNames) it returns each var's value joined
// with a space in input order, or "" if any var is undefined so the caller
// can reject the buffer entry instead of silently substituting "$VAR".
func resolveBufferVars(pp shell.ParsedParam, vars []vars.Variable) string {
	resolved := func(name string) (string, bool) {
		for _, v := range vars {
			if v.Name == name {
				return v.Value, true
			}
		}
		return "", false
	}
	if len(pp.VarNames) > 0 {
		parts := make([]string, 0, len(pp.VarNames))
		for _, name := range pp.VarNames {
			v, ok := resolved(name)
			if !ok {
				return ""
			}
			parts = append(parts, v)
		}
		return strings.Join(parts, " ")
	}
	if pp.VarName != "" {
		v, ok := resolved(pp.VarName)
		if !ok {
			return ""
		}
		return v
	}
	return ""
}

// matchesParamVar reports whether v matches p per the §4.2 heuristic. Used
// by the v-key toggle handler to re-discover the variable name when the user
// switches back from literal to var mode.
func matchesParamVar(v vars.Variable, p metadata.Parameter) bool {
	matches := vars.MatchVariables([]vars.Variable{v}, []metadata.Parameter{p})
	return len(matches) == 1 && matches[0].ParamName == p.Name
}
