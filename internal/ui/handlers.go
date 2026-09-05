package ui

import (
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/state"
	"github.com/someson/azform/internal/validate"
	"github.com/someson/azform/internal/vars"
)

func (m Form) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {

	case FormModeEnum:
		var cmd tea.Cmd
		m.enumPop, cmd = m.enumPop.Update(msg)
		return m, cmd

	case FormModeVarPick:
		var cmd tea.Cmd
		m.varPop, cmd = m.varPop.Update(msg)
		return m, cmd

	case FormModeEdit:
		switch msg.String() {
		case "esc":
			m.declaring = false
			m.mode = FormModeList
			return m, nil
		case "ctrl+g":
			// Open the variable picker ("g" for "get variable"). Avoids
			// Ctrl+V so the user's terminal paste binding keeps working.
			// The textinput keeps its current value; on pick we splice
			// `$NAME` at the cursor and return to edit mode. On cancel
			// we return with everything intact.
			names := uniqueVarNames(m.src.Vars, m.sessionVars)
			m.varEditCursor = m.textInput.Position()
			m.varPop = NewVarPicker(names, m.width)
			m.mode = FormModeVarPick
			return m, nil
		case "enter":
			m.fields[m.editIdx].Value = m.textInput.Value()
			if m.textInput.Value() != "" {
				m.fields[m.editIdx].Enabled = true
			}
			m.recomputeFindings(nil)
			if m.declaring {
				if f := &m.fields[m.editIdx]; f.Source == FieldSourceEnv && f.VarValue != "" {
					if name := varNameFor(f.VarValue, m.src.Vars); name != "" {
						m.declaredVars = append(m.declaredVars, DeclaredVar{Name: name, Value: f.Value})
					}
				}
				m.declaring = false
			}
			m.mode = FormModeList
			return m, nil
		default:
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}

	case FormModeFilter:
		switch msg.String() {
		case "esc":
			m.filterQuery = ""
			m.filterInput.SetValue("")
			m.mode = FormModeList
			m.rebuildVisible()
			return m, nil
		case "enter":
			if len(m.visible) == 0 {
				// Refuse to leave filter mode while zero fields match —
				// otherwise the user lands in an empty list with no
				// obvious way back to the query.
				return m, nil
			}
			m.mode = FormModeList
			return m, nil
		default:
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.filterQuery = m.filterInput.Value()
			m.rebuildVisible()
			return m, cmd
		}

	case FormModeHelp:
		// Any keypress dismisses the cheatsheet. We don't filter for
		// "q"/"esc"/"?" specifically — even arrow keys feel natural here
		// because the user is just previewing, not navigating.
		m.mode = FormModeList
		return m, nil

	case FormModeDone:
		switch msg.String() {
		case "enter":
			return m.confirmDone()
		case "tab":
			m.mode = FormModeCancel
			return m, nil
		case "shift+tab":
			m.mode = FormModeList
			return m, nil
		case "esc", "?", "f1":
			m.mode = FormModeHelp
			return m, nil
		}

	case FormModeCancel:
		switch msg.String() {
		case "enter":
			return m.confirmCancel()
		case "tab":
			m.mode = FormModeList
			return m, nil
		case "shift+tab":
			m.mode = FormModeDone
			return m, nil
		case "esc", "?", "f1":
			m.mode = FormModeHelp
			return m, nil
		}

	case FormModeList:
		switch msg.String() {
		case "up", "k":
			if m.moveCursorVert(-1) {
				if idx := m.fieldAt(m.cursor); idx >= 0 {
					return m, m.maybeFetchField(idx)
				}
			}
			return m, nil
		case "down", "j":
			if m.moveCursorVert(+1) {
				if idx := m.fieldAt(m.cursor); idx >= 0 {
					return m, m.maybeFetchField(idx)
				}
			}
			return m, nil
		case " ":
			idx := m.fieldAt(m.cursor)
			if idx >= 0 {
				if m.fields[idx].Param.Required {
					// Required params cannot be toggled off. Explain instead
					// of silently dropping the keystroke (spec §6.6).
					m.hintMsg = m.fields[idx].Param.Name + " is required"
					m.hintActive = true
					return m, tea.Tick(hintDuration, func(time.Time) tea.Msg { return HintClearMsg{} })
				}
				m.fields[idx].Enabled = !m.fields[idx].Enabled
				m.hintMsg = ""
				m.recomputeFindings(nil)
			}
			return m, nil
		case "enter":
			idx := m.fieldAt(m.cursor)
			if idx < 0 {
				return m, nil
			}
			f := &m.fields[idx]
			// Bare switches (--no-wait, --debug, etc.) have no value —
			// Enter toggles them on/off, matching space. Skip the picker.
			if f.Param.IsSwitch() {
				if f.Param.Required {
					m.hintMsg = f.Param.Name + " is required"
					m.hintActive = true
					return m, tea.Tick(hintDuration, func(time.Time) tea.Msg { return HintClearMsg{} })
				}
				f.Enabled = !f.Enabled
				f.Value = ""
				m.hintMsg = ""
				m.recomputeFindings(nil)
				return m, nil
			}
			switch f.Param.ValueKind {
			case metadata.ValueKindEnum, metadata.ValueKindBool:
				// Prefer lazily fetched choices over static metadata (spec §6.1).
				choices := f.FetchedChoices
				if len(choices) == 0 {
					choices = f.Param.Choices
				}
				if f.Param.ValueKind == metadata.ValueKindBool {
					choices = []string{"true", "false"}
				}
				m.enumPop = NewEnum(choices, f.Value, m.width-4)
				m.enumIdx = idx
				m.mode = FormModeEnum
			default:
				m.textInput.SetValue(f.Value)
				// Constrain the input to the grid value column so the
				// in-place edit (renderGridCell replaces the value cell
				// with the textinput) fits without overflowing into the
				// next column. Single-column mode leaves the input at its
				// natural width so long values can be edited without
				// horizontal scrolling.
				if _, _, cols := m.gridLayout(); cols >= 2 {
					// Reserve 1 cell for the cursor so bubbles/textinput's
					// View() output width stays within gridValueBudget and
					// doesn't overflow into the next grid column.
					m.textInput.Width = gridValueBudget - 1
				} else {
					m.textInput.Width = 0
				}
				focusCmd := m.textInput.Focus()
				m.editIdx = idx
				m.mode = FormModeEdit
				return m, focusCmd
			}
			return m, nil
		case "g":
			m.showGlobals = !m.showGlobals
			return m, nil
		case "h", "left":
			if m.moveCursorHoriz(-1) {
				if idx := m.fieldAt(m.cursor); idx >= 0 {
					return m, m.maybeFetchField(idx)
				}
			}
			return m, nil
		case "l", "right":
			if m.moveCursorHoriz(+1) {
				if idx := m.fieldAt(m.cursor); idx >= 0 {
					return m, m.maybeFetchField(idx)
				}
			}
			return m, nil
		case "/":
			// Reset the filter on every open. Without this, pressing /
			// again shows the empty input but m.filterQuery still holds
			// the previous query, so visible stays narrowed and the user
			// can't tell whether typing will update anything until they
			// hit a key. Clearing here mirrors the esc-out behaviour and
			// keeps the cursor on the same field (rebuildVisible preserves
			// the cursor when its field is still in the new visible set).
			m.filterQuery = ""
			m.rebuildVisible()
			m.filterInput.SetValue("")
			var focusCmd tea.Cmd
			focusCmd = m.filterInput.Focus()
			m.mode = FormModeFilter
			return m, focusCmd
		case "v":
			idx := m.fieldAt(m.cursor)
			// Closed choice sets (enum/bool) never take var mode — az expects a
			// literal from the set, so there is nothing to toggle into.
			if idx >= 0 && m.fields[idx].Source == FieldSourceEnv && len(m.fields[idx].Param.Choices) == 0 {
				if m.fields[idx].Mode == FieldModeVar {
					m.fields[idx].Mode = FieldModeLiteral
					m.fields[idx].Value = m.fields[idx].VarValue
				} else {
					m.fields[idx].Mode = FieldModeVar
					for _, vv := range m.src.Vars {
						if vv.Value == m.fields[idx].VarValue && matchesParamVar(vv, m.fields[idx].Param) {
							m.fields[idx].Value = "$" + vv.Name
							break
						}
					}
				}
				m.recomputeFindings(nil)
			}
			return m, nil
		case "d":
			idx := m.fieldAt(m.cursor)
			if idx >= 0 && m.fields[idx].Mode == FieldModeVar && m.fields[idx].Source == FieldSourceEnv {
				m.fields[idx].Mode = FieldModeLiteral
				m.fields[idx].Value = m.fields[idx].VarValue
				m.textInput.SetValue(m.fields[idx].Value)
				focusCmd := m.textInput.Focus()
				m.editIdx = idx
				m.declaring = true
				m.mode = FormModeEdit
				return m, focusCmd
			}
			return m, nil
		case "tab":
			m.mode = FormModeDone
			return m, nil
		case "shift+tab":
			m.mode = FormModeCancel
			return m, nil
		case "w":
			// Cycle through non-blocking warnings in the footer. The footer
			// only has one warning slot, so 'w' advances to the next; after
			// the last it wraps back to the first. No-op when there are no
			// warnings.
			if total := len(m.warningFindings()); total > 1 {
				m.warningIdx = (m.warningIdx + 1) % total
			}
			return m, nil
		case "?", "f1":
			m.mode = FormModeHelp
			return m, nil
		case "esc", "q":
			// q is an alias for Esc in list mode (quit / cancel fetch). It is
			// deliberately not aliased in edit/filter modes, where q must
			// remain typeable text.
			if m.cancelFetchIdx >= 0 && m.cancelFetchIdx < len(m.fields) {
				idx := m.cancelFetchIdx
				m.fields[idx].FetchState = FetchIdle
				m.fields[idx].FetchSpinnerShow = false
				m.cancelFetchIdx = -1
				m.slowFetchIdx = -1
				m.hintMsg = fetchCancelledHint
				m.hintActive = true
				return m, nil
			}
			return m.confirmCancel()
		}
	}
	return m, nil
}

func (m Form) confirmDone() (tea.Model, tea.Cmd) {
	m.recomputeFindings(nil)
	for _, f := range m.findings {
		if f.Severity == validate.SeverityBlocking {
			m.errorMsg = f.Message
			return m, nil
		}
	}
	m.errorMsg = ""
	m.result = m.buildCommand()
	m.persistBindings()
	_ = m.draftStore.Delete(m.command)
	return m, tea.Quit
}

// persistBindings records each var-mode field as a binding (spec §8.1).
// For enum params the resolved literal value is also stored; for non-enum
// string params only the var name is recorded. Literal-mode non-enum values
// are not persisted (sensitive / arbitrary strings).
func (m Form) persistBindings() {
	if m.src.Bindings == nil {
		return
	}
	for _, f := range m.fields {
		if f.Mode != FieldModeVar {
			continue
		}
		name := extractVarName(f.Value)
		if name == "" {
			continue
		}
		key := state.BindingKey(m.command, f.Param.Name)
		c := state.Candidate{Name: name, Uses: 1}
		if f.Param.ValueKind == metadata.ValueKindEnum {
			c.Value = f.VarValue
		}
		_ = m.src.Bindings.Touch(key, c)
	}
}

// extractVarName returns the var name from "$NAME" or "${NAME}", or "" if
// the value is not a var reference (including command substitution).
func extractVarName(value string) string {
	if len(value) > 1 && value[0] == '$' && value[1] != '(' {
		if value[1] == '{' && len(value) > 2 && value[len(value)-1] == '}' {
			return value[2 : len(value)-1]
		}
		return value[1:]
	}
	return ""
}

func varNameFor(value string, in []vars.Variable) string {
	for _, v := range in {
		if v.Value == value {
			return v.Name
		}
	}
	return ""
}

// shellInternalsPrefixes is the blocklist of prefixes that zsh, macOS,
// and standard Unix tooling use to expose built-in state. Names matching
// any of these are filtered out of the picker. User vars (RG, LOC, SA,
// MYVAR, MYPROJECT, etc.) almost always fail to match and survive.
//
// Keep this list small and well-justified. False positives (a real user
// var that happens to start with one of these) are easy to fix: the
// user types $IT directly into the textinput. False negatives (env
// vars that leak through) pollute every picker's display, so err on
// the side of filtering more.
var shellInternalsPrefixes = []string{
	// zsh internals
	"ZSH", "ZLE", "TRY_BLOCK", "WIDGET", "WATCH",
	"WIDGETFUNC", "WIDGETSTYLE", "WATCHFMT", "SUFFIX",
	"ZSH_THEME", "ZSH_ARGZERO", "ZSH_EVAL",
	// macOS / system / locale
	"XPC", "DYLD", "LD_", "LC_", "LANG", "LANGUAGE",
	"XDG", "DBUS", "GDMSESSION", "GDM_",
	"DISPLAY", "XAUTHORITY",
	"CLUTTER", "GTK", "QT", "IM_",
	// terminals / editors
	"TERM", "TERMINFO", "COLORTERM",
	"SSH_", "HOSTNAME", "HOSTTYPE", "OSTYPE", "MACHTYPE", "VENDOR",
	"EDITOR", "VISUAL", "PAGER", "BROWSER", "LESS",
	// user / process state
	"USER", "USERNAME", "UID", "GID",
	"SHELL", "SHLVL",
	"PWD", "OLDPWD", "CDPATH", "FPATH", "MANPATH", "INFOPATH",
	"HOME", "TMPDIR", "TMP", "TEMP",
	// Common short env exports that the length heuristic misses.
	// User vars of this shape (RG, LOC, SA) almost always appear in
	// the prefix-aware portion of the table above; if your user var
	// shares a name with these, add it to shellInternalsKeep below.
	"PATH", "HOST", "PORT",
	// history / zle state
	"HIST", "YANK", "UNDO", "BUFFER", "ARGC",
	// language toolchains
	"PYTHON", "PERL", "RUBY", "NODE", "GO", "CARGO", "JAVA",
	"ANDROID", "CLASSPATH", "LSCOLORS", "LS_COLORS",
	// misc
	"COLUMNS", "LINES", "MAIL", "MAILCHECK", "SECONDS",
	"LINENO", "TTY", "VCS",
	// zsh prompt machinery
	"PROMPT", "RPROMPT", "POWERLEVEL9K", "P9K",
}

// isShellInternal reports whether name looks like a zsh / macOS shell
// export the user is unlikely to insert as a $VAR reference.
//
// Two heuristics:
//
//  1. Names starting with `_` — long-standing convention for internal
//     state (zsh internals, completion helpers, plugin config like
//     p10k's `_p9k__*`).
//  2. Names starting with any shellInternalsPrefixes entry — covers
//     the common uppercase-with-underscore prefixes that zsh, macOS,
//     and standard tooling use to expose built-in state (ZSH, ZLE,
//     TERM, XDG, SSH, PYTHON, …).
//
// We deliberately don't add length/underscore heuristics: those would
// also catch legitimate user vars (PROJECT_NAME, MYAPP_DB_URL, etc.).
// The blocklist of known shell prefixes is more accurate at the cost
// of occasionally missing a new shell export. False negatives are
// easy to fix (just type `$IT` directly); false positives are
// confusing.
func isShellInternal(name string) bool {
	if strings.HasPrefix(name, "_") {
		return true
	}
	// zsh special parameters (!, #, $, -, 0, ?, ARGC, status, …) and
	// other non-identifier names are useless as $VAR references; a name
	// a user would reference always starts with a letter.
	r, _ := utf8.DecodeRuneInString(name)
	if !unicode.IsLetter(r) {
		return true
	}
	// Single-letter names are zsh specials or noise; real user vars
	// are longer.
	if len(name) < 2 {
		return true
	}
	for _, p := range shellInternalsPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// uniqueVarNames returns the deduped, sorted set of variable names the
// widget can offer in the picker: every var from m.src.Vars plus every
// session var name. The picker only needs names (values are looked up
// later when the inserted `$NAME` is matched against params).
//
// Two filters strip shell/system noise so the picker shows only vars
// the user is likely to want to reference:
//
//  1. Names starting with `_` — long-standing convention for internal
//     state (zsh internals, completion helpers, plugin config like
//     p10k's `_p9k__*`).
//  2. Names that look like zsh / macOS system exports — known prefixes
//     (ZSH_, ZLE_, TERM_, …) and very long uppercase-with-underscore
//     strings are almost always theme / module state, not user vars.
//
// User vars almost always survive both filters; if a real var gets
// caught, the user can still type `$IT` by hand in the textinput.
func uniqueVarNames(in []vars.Variable, session map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if v.Name == "" || isShellInternal(v.Name) || seen[v.Name] {
			continue
		}
		seen[v.Name] = true
		out = append(out, v.Name)
	}
	for n := range session {
		if n == "" || isShellInternal(n) || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (m Form) confirmCancel() (tea.Model, tea.Cmd) {
	values, disabled := m.collectFieldValues()
	_ = m.draftStore.SaveWithDisabled(m.command, values, disabled)
	m.quitting = true
	return m, tea.Quit
}
