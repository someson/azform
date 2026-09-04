package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// VarStatus classifies the runtime availability of a field's var reference.
type VarStatus int

const (
	VarStatusNone    VarStatus = iota // not a var reference
	VarStatusGreen                    // var is defined in the current shell
	VarStatusGray                     // var is referenced but not in this shell
	VarStatusNeutral                  // $(…) command substitution — no validation
)

var (
	statusGreenStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	statusGrayStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red — "this var is referenced but not defined; az will receive an empty/errored value at exec"
	statusNeutralStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
)

// StatusOf inspects value and reports whether it is a var ref that is
// currently defined in the shell (green), a var ref that is not (gray), or a
// command substitution that we never validate (neutral).
func StatusOf(value string, sessionVars map[string]bool) VarStatus {
	if value == "" {
		return VarStatusNone
	}
	if strings.HasPrefix(value, "$(") {
		return VarStatusNeutral
	}
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		name := value[2 : len(value)-1]
		if isVarName(name) {
			if sessionVars[name] {
				return VarStatusGreen
			}
			return VarStatusGray
		}
		return VarStatusNone
	}
	if strings.HasPrefix(value, "$") && len(value) > 1 && value[1] != '(' {
		name := value[1:]
		if isVarName(name) {
			if sessionVars[name] {
				return VarStatusGreen
			}
			return VarStatusGray
		}
	}
	return VarStatusNone
}

// Style returns the lipgloss style matching the status.
func (s VarStatus) Style() lipgloss.Style {
	switch s {
	case VarStatusGreen:
		return statusGreenStyle
	case VarStatusGray:
		return statusGrayStyle
	case VarStatusNeutral:
		return statusNeutralStyle
	default:
		return lipgloss.NewStyle()
	}
}
