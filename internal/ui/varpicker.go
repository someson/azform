package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// VarPickedMsg is sent when the user picks a variable from the var picker
// popup. Name is the variable name (without the leading `$`).
type VarPickedMsg struct{ Name string }

// VarPickerCancelledMsg is sent when the user dismisses the var picker.
type VarPickerCancelledMsg struct{}

// pickColGap is the horizontal gap between adjacent var-picker columns
// inside the unified bordered box.
const pickColGap = 2

// pickColMinWidth is the floor for one column's content width.
const pickColMinWidth = 12

// pickerRows is the fixed interior height of the unified var picker box.
const pickerRows = 5

// VarPickerModel is a bubbletea component for picking a variable
// reference. Variables are arranged column-major inside a single
// bordered box that spans the terminal width; columns beyond the
// visible window scroll horizontally.
type VarPickerModel struct {
	vars        []string // variable names without leading `$`
	colIdx      int      // global column index (0..cols-1)
	rowIdx      int      // 0..rows-1 within the current column
	cols        int      // total columns = ceil(len(vars)/rows)
	visibleCols int      // how many cols fit in the terminal width
	colOffset   int      // leftmost visible column
	colW        int      // per-column content width (no borders/gaps)
	rows        int      // interior rows (fixed = pickerRows)
}

// NewVarPicker creates a VarPickerModel from a list of variable names.
func NewVarPicker(names []string, availableWidth int) VarPickerModel {
	p := VarPickerModel{vars: names, rows: pickerRows}
	p.layout(availableWidth)
	return p
}

// layout recomputes cols/visibleCols/colW/colOffset for the given
// terminal width. Safe to call again on window resize.
func (m *VarPickerModel) layout(availableWidth int) {
	if m.rows <= 0 {
		m.rows = pickerRows
	}
	if len(m.vars) == 0 {
		m.cols = 0
		m.visibleCols = 0
		m.colOffset = 0
		m.colW = pickColMinWidth
		return
	}
	// Column content width: max(pickColMinWidth, longest_name + 4).
	colW := pickColMinWidth
	for _, n := range m.vars {
		if w := runewidth.StringWidth(n) + 4; w > colW {
			colW = w
		}
	}
	m.colW = colW
	m.cols = (len(m.vars) + m.rows - 1) / m.rows

	// Interior width = availableWidth - 2 (borders).
	innerWidth := availableWidth - 2
	if innerWidth < colW {
		m.visibleCols = 1
	} else {
		m.visibleCols = (innerWidth + pickColGap) / (colW + pickColGap)
		if m.visibleCols < 1 {
			m.visibleCols = 1
		}
	}
	if m.visibleCols > m.cols {
		m.visibleCols = m.cols
	}
	if m.colIdx < 0 {
		m.colIdx = 0
	}
	if m.colIdx >= m.cols {
		m.colIdx = m.cols - 1
	}
	if m.rowIdx < 0 {
		m.rowIdx = 0
	}
	if r := m.colRows(m.colIdx); m.rowIdx >= r {
		m.rowIdx = r - 1
	}
	m.ensureVisible()
}

// ensureVisible shifts colOffset so colIdx is within
// [colOffset, colOffset+visibleCols).
func (m *VarPickerModel) ensureVisible() {
	if m.visibleCols <= 0 {
		m.colOffset = 0
		return
	}
	if m.colIdx < m.colOffset {
		m.colOffset = m.colIdx
	}
	if m.colIdx >= m.colOffset+m.visibleCols {
		m.colOffset = m.colIdx - m.visibleCols + 1
	}
	if m.colOffset < 0 {
		m.colOffset = 0
	}
	if maxOffset := m.cols - m.visibleCols; m.colOffset > maxOffset && maxOffset >= 0 {
		m.colOffset = maxOffset
	}
}

// colRows returns how many entries the i-th column holds.
func (m VarPickerModel) colRows(i int) int {
	if i < 0 || i >= m.cols {
		return 0
	}
	remaining := len(m.vars) - i*m.rows
	if remaining < 0 {
		return 0
	}
	if remaining > m.rows {
		return m.rows
	}
	return remaining
}

// colEntries returns the slice of vars in the i-th column.
func (m VarPickerModel) colEntries(i int) []string {
	if i < 0 || i >= m.cols {
		return nil
	}
	start := i * m.rows
	end := start + m.colRows(i)
	return m.vars[start:end]
}

// selectedVar returns the var under the cursor, or "".
func (m VarPickerModel) selectedVar() string {
	entries := m.colEntries(m.colIdx)
	if m.rowIdx < 0 || m.rowIdx >= len(entries) {
		return ""
	}
	return entries[m.rowIdx]
}

// Init implements tea.Model.
func (m VarPickerModel) Init() tea.Cmd { return nil }

// Update handles navigation, pick, and cancel keys.
func (m VarPickerModel) Update(msg tea.Msg) (VarPickerModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "up", "k":
		if m.rowIdx > 0 {
			m.rowIdx--
		}
	case "down", "j":
		if m.rowIdx < m.colRows(m.colIdx)-1 {
			m.rowIdx++
		}
	case "left", "h":
		if m.colIdx > 0 {
			m.colIdx--
			if m.rowIdx >= m.colRows(m.colIdx) {
				m.rowIdx = m.colRows(m.colIdx) - 1
			}
			m.ensureVisible()
		}
	case "right", "l":
		if m.colIdx < m.cols-1 {
			m.colIdx++
			if m.rowIdx >= m.colRows(m.colIdx) {
				m.rowIdx = m.colRows(m.colIdx) - 1
			}
			m.ensureVisible()
		}
	case "enter":
		if name := m.selectedVar(); name != "" {
			return m, func() tea.Msg { return VarPickedMsg{Name: name} }
		}
		return m, func() tea.Msg { return VarPickerCancelledMsg{} }
	case "esc", "q":
		return m, func() tea.Msg { return VarPickerCancelledMsg{} }
	}
	return m, nil
}

// PickerColNames returns one column's entries.
func (m VarPickerModel) PickerColNames(i int) []string { return m.colEntries(i) }

// PickerCursor returns the (colIdx, rowIdx) tuple.
func (m VarPickerModel) PickerCursor() (int, int) { return m.colIdx, m.rowIdx }

// PickerCols returns the total column count.
func (m VarPickerModel) PickerCols() int { return m.cols }

// VisibleCols returns how many columns are currently visible.
func (m VarPickerModel) VisibleCols() int { return m.visibleCols }

// ColOffset returns the leftmost visible column index.
func (m VarPickerModel) ColOffset() int { return m.colOffset }

// ColW returns the per-column content width.
func (m VarPickerModel) ColW() int { return m.colW }

// TotalCols is an alias for PickerCols for test clarity.
func (m VarPickerModel) TotalCols() int { return m.cols }

// Rows returns the interior row count (fixed at pickerRows).
func (m VarPickerModel) Rows() int { return m.rows }
