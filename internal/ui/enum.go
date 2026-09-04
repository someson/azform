package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EnumSelectedMsg is sent when the user picks a value from the enum popup.
type EnumSelectedMsg struct{ Value string }

// EnumCancelledMsg is sent when the user presses Esc in the enum popup.
type EnumCancelledMsg struct{}

var (
	enumCursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
)

// EnumModel is a bubbletea component for picking from a closed list of choices.
// It is embedded in Form when the user opens an enum or bool field.
type EnumModel struct {
	choices []string
	cursor  int
	width   int
}

// NewEnum creates an EnumModel pre-positioned at current (empty string → index 0).
func NewEnum(choices []string, current string, width int) EnumModel {
	cursor := 0
	for i, c := range choices {
		if c == current {
			cursor = i
			break
		}
	}
	return EnumModel{choices: choices, cursor: cursor, width: width}
}

// Init implements tea.Model.
func (m EnumModel) Init() tea.Cmd { return nil }

// Update handles ↑/↓ navigation, Enter (select), and Esc/q (cancel).
// Esc and q emit EnumCancelledMsg; Enter emits EnumSelectedMsg. Neither key
// propagates out of the enum popup (spec 6.6 — Esc must not close the form).
func (m EnumModel) Update(msg tea.Msg) (EnumModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.choices)-1 {
			m.cursor++
		}
	case "enter":
		val := m.choices[m.cursor]
		return m, func() tea.Msg { return EnumSelectedMsg{Value: val} }
	case "esc", "q":
		return m, func() tea.Msg { return EnumCancelledMsg{} }
	}
	return m, nil
}

// View renders the choice list. The selected row is highlighted.
func (m EnumModel) View() string {
	var sb strings.Builder
	for i, c := range m.choices {
		if i == m.cursor {
			sb.WriteString(enumCursorStyle.Render("  ▶ " + c))
		} else {
			sb.WriteString("    ")
			sb.WriteString(c)
		}
		if i < len(m.choices)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
