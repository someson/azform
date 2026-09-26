package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EnumSelectedMsg is sent when the user picks a value from the enum popup.
// Query is the popup's search text at the time, so picking the free-text
// row can hand what was typed on to the editor.
type EnumSelectedMsg struct {
	Value string
	Query string
}

// EnumCancelledMsg is sent when the user presses Esc in the enum popup.
type EnumCancelledMsg struct{}

var (
	enumCursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
)

// EnumModel is a bubbletea component for picking from a list of choices.
// It is embedded in Form for enum/bool fields and for fetched values.
//
// "/" starts a case-insensitive substring search, the same key as the
// form's own filter. While searching, typed characters (j, k and q
// included) go into the query and the arrow keys move; Esc ends the
// search before a second Esc closes the popup. A leading manualEntryChoice
// row is pinned: it stays visible whatever the query.
type EnumModel struct {
	all       []string // every choice, pinned row included
	choices   []string // the rows currently shown (all, narrowed by query)
	cursor    int      // index into choices
	width     int
	query     string
	searching bool
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
	return EnumModel{all: choices, choices: choices, cursor: cursor, width: width}
}

// Init implements tea.Model.
func (m EnumModel) Init() tea.Cmd { return nil }

// Update handles navigation, search, Enter (select) and Esc/q (cancel).
// Esc and q emit EnumCancelledMsg; Enter emits EnumSelectedMsg. Neither key
// propagates out of the enum popup (spec 6.6 — Esc must not close the form).
func (m EnumModel) Update(msg tea.Msg) (EnumModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.searching {
		return m.updateSearch(km)
	}
	switch km.String() {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(+1)
	case "/":
		m.searching = true
	case "enter":
		return m, m.selected()
	case "esc", "q":
		return m, func() tea.Msg { return EnumCancelledMsg{} }
	}
	return m, nil
}

func (m EnumModel) updateSearch(km tea.KeyMsg) (EnumModel, tea.Cmd) {
	switch km.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		m.move(-1)
	case tea.KeyDown, tea.KeyCtrlN:
		m.move(+1)
	case tea.KeyEnter:
		return m, m.selected()
	case tea.KeyEsc:
		m.searching = false
		m.setQuery("")
	case tea.KeyBackspace:
		if r := []rune(m.query); len(r) > 0 {
			m.setQuery(string(r[:len(r)-1]))
		}
	case tea.KeyRunes, tea.KeySpace:
		m.setQuery(m.query + string(km.Runes))
	}
	return m, nil
}

func (m *EnumModel) move(delta int) {
	if next := m.cursor + delta; next >= 0 && next < len(m.choices) {
		m.cursor = next
	}
}

func (m EnumModel) selected() tea.Cmd {
	if len(m.choices) == 0 {
		return nil
	}
	val, query := m.choices[m.cursor], m.query
	return func() tea.Msg { return EnumSelectedMsg{Value: val, Query: query} }
}

// setQuery narrows choices to the case-insensitive matches of q and puts
// the cursor on the first real match (after the pinned row, if any), so
// Enter right after typing picks what the user was looking for.
func (m *EnumModel) setQuery(q string) {
	m.query = q
	needle := strings.ToLower(q)
	m.choices = m.choices[:0:0]
	firstMatch := -1
	for i, c := range m.all {
		pinned := i == 0 && c == manualEntryChoice
		if pinned || strings.Contains(strings.ToLower(c), needle) {
			if !pinned && firstMatch < 0 {
				firstMatch = len(m.choices)
			}
			m.choices = append(m.choices, c)
		}
	}
	m.cursor = 0
	if firstMatch >= 0 {
		m.cursor = firstMatch
	}
}

// Header is the extra popup row above the choices: the live query while
// searching, a "/ filter" hint when the list is long enough to scroll,
// else "".
func (m EnumModel) Header() string {
	switch {
	case m.searching || m.query != "":
		return "/ " + m.query + "▏"
	case len(m.all) > maxPopupItems:
		return "/ filter"
	}
	return ""
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
