package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/someson/azform/internal/render"
	"github.com/someson/azform/internal/validate"
)

// View satisfies tea.Model. Renders the header, the grid or single-column
// body, the preview line, and the findings footer.
func (m Form) View() string {
	switch m.loadState {
	case LoadStateLoading:
		return m.spin.View() + " loading command metadata…\n"
	case LoadStateError:
		return errStyle.Render("error: "+m.loadErr) + "\n\nPress any key to exit.\n"
	}
	if m.mode == FormModeHelp {
		return m.renderHelp()
	}

	var sb strings.Builder
	w := m.width
	if w == 0 {
		w = 80
	}
	sep := strings.Repeat("─", w)

	writeLine(&sb, headerStyle.Render("az "+m.command))
	if m.summary != "" {
		// Wrap the command description to the terminal width so long
		// summaries stay fully visible instead of being clipped.
		writeLine(&sb, ansi.Wrap(m.summary, w, " "))
	}
	if m.staleWarn != "" {
		writeLine(&sb, warnStyle.Render(m.staleWarn))
	}
	writeLine(&sb, sep)

	// Decide layout. Grid stays active in every mode — text edits render
	// inside the focused cell (replacing the value column) and enum
	// popups float as overlays below it. The previous behaviour forced
	// a single-column reflow on every edit, which reflowed the rows the
	// user was trying to keep their bearings on.
	roCols, globalsCol, cols := m.gridLayout()
	gridMode := cols >= 2

	var focusedFullValue string
	var focusedCol, focusedRow int
	// visibleLines holds the post-viewport body lines for grid mode so we
	// can splice the enum popup overlay into them at the focused row
	// before writing to sb. Nil in single-column mode.
	var visibleLines []string
	var cellWidths []int
	if gridMode {
		body, cursorRow, col, fullVal := m.renderGrid(roCols, globalsCol)
		focusedFullValue = fullVal
		focusedCol = col
		focusedRow = cursorRow
		if m.vpReady {
			m.vp.SetContent(body)
			if cursorRow >= 0 {
				top, bottom := m.vp.YOffset, m.vp.YOffset+m.vp.Height-1
				switch {
				case cursorRow < top:
					m.vp.SetYOffset(cursorRow)
				case cursorRow > bottom:
					m.vp.SetYOffset(cursorRow - m.vp.Height + 1)
				}
			}
			// Render only the visible scroll window — no padding to vp.Height.
			// vp.View() pads with blanks, producing a large gap between the
			// form and the preview line when content < available. Splice
			// [YOffset, YOffset+Height) from the body and emit as-is.
			lines := strings.Split(body, "\n")
			start, end := m.vp.YOffset, m.vp.YOffset+m.vp.Height
			if start < 0 {
				start = 0
			}
			if start > len(lines) {
				start = len(lines)
			}
			if end > len(lines) {
				end = len(lines)
			}
			if end > start {
				visibleLines = lines[start:end]
			}
		} else {
			visibleLines = strings.Split(body, "\n")
		}
		// Recompute cell widths so the overlay code can position the popup
		// at the focused column's left edge.
		all := append([][]int(nil), roCols...)
		if len(globalsCol) > 0 {
			all = append(all, globalsCol)
		}
		cellWidths = make([]int, len(all))
		for c, col := range all {
			w := gridMinNameCol
			for _, idx := range col {
				if n := len(m.fields[idx].Param.Name); n > w {
					w = n
				}
			}
			cellWidths[c] = 2 + w + 1 + gridValueBudget
		}

		// Enum / var-picker popup overlay: when the user opens a
		// closed-choice field in grid mode, splice the popup over the
		// rows immediately below the focused cell so the grid stays put.
		// The popup is anchored at the focused cell's column. When the
		// viewport can't hold the full popup below the focused row, we
		// grow visibleLines with blank rows so the popup always renders
		// completely — pushing the preview line down rather than
		// silently clipping the popup's bottom border.
		if focusedRow >= 0 && focusedCol >= 0 {
			var popupHeight int
			switch m.mode {
			case FormModeEnum:
				popupHeight = enumPopupHeight(m.enumPop.choices)
			case FormModeVarPick:
				popupHeight = len(m.buildVarPickerBox())
			}
			if popupHeight > 0 {
				visibleIdx := focusedRow - m.vp.YOffset
				if visibleIdx >= 0 {
					needed := visibleIdx + 1 + popupHeight
					for len(visibleLines) < needed {
						visibleLines = append(visibleLines, "")
					}
				}
			}
			switch m.mode {
			case FormModeEnum:
				visibleLines = m.spliceEnumOverlay(visibleLines, focusedRow, focusedCol, cellWidths)
			case FormModeVarPick:
				visibleLines = m.spliceVarOverlay(visibleLines, focusedRow, focusedCol, cellWidths)
			}
		}

		if len(visibleLines) > 0 {
			sb.WriteString(strings.Join(visibleLines, "\n"))
			sb.WriteByte('\n')
		}
	} else {
		// Single-column fallback: required rows flow above optional, no
		// separator — red/green colouring distinguishes them.
		nameWidth := m.nameColumnWidth()
		for _, idx := range m.reqIndices {
			// Apply the active filter to required rows as well. Without
			// this, the required section always renders every required
			// field regardless of query — searching "server" on a command
			// with three required params leaves you with 3 noisy rows on
			// top of the actual match, which feels broken. Done still
			// blocks on missing required values, so hiding them while
			// filtering is safe.
			if m.filterQuery != "" && !matchesFilter(m.fields[idx], m.filterQuery) {
				continue
			}
			writeLine(&sb, m.renderFieldSelected(idx, m.fieldAt(m.cursor) == idx, nameWidth))
		}

		switch m.mode {
		case FormModeEnum:
			writeLine(&sb, m.enumPop.View())
		case FormModeVarPick:
			// Unified bordered box rendered full-width. Same geometry as
			// the grid-mode overlay so both modes look consistent.
			for _, ln := range m.buildVarPickerBox() {
				writeLine(&sb, ln)
			}
		default:
			var optSB strings.Builder
			hiddenGlobals := 0
			cursorRow := -1
			row := 0
			focusIdx := m.fieldAt(m.cursor)
			for _, idx := range m.visible {
				f := &m.fields[idx]
				if f.Param.Required {
					continue
				}
				if f.Param.Global && !m.showGlobals && !f.Enabled {
					hiddenGlobals++
					continue
				}
				if idx == focusIdx {
					cursorRow = row
				}
				writeLine(&optSB, m.renderFieldSelected(idx, idx == focusIdx, nameWidth))
				row++
			}
			if hiddenGlobals > 0 {
				writeLine(&optSB, hintStyle.Render(fmt.Sprintf("  (press g to show %d global argument(s))", hiddenGlobals)))
			}
			if m.vpReady {
				m.vp.SetContent(optSB.String())
				if cursorRow >= 0 {
					top, bottom := m.vp.YOffset, m.vp.YOffset+m.vp.Height-1
					switch {
					case cursorRow < top:
						m.vp.SetYOffset(cursorRow)
					case cursorRow > bottom:
						m.vp.SetYOffset(cursorRow - m.vp.Height + 1)
					}
				}
				// Same un-padded splice as grid mode — see comment above.
				lines := strings.Split(optSB.String(), "\n")
				start, end := m.vp.YOffset, m.vp.YOffset+m.vp.Height
				if start < 0 {
					start = 0
				}
				if start > len(lines) {
					start = len(lines)
				}
				if end > len(lines) {
					end = len(lines)
				}
				if end > start {
					sb.WriteString(strings.Join(lines[start:end], "\n"))
					sb.WriteByte('\n')
				}
			} else {
				sb.WriteString(optSB.String())
			}
		}
	}

	if m.mode == FormModeFilter {
		writeLine(&sb, sep)
		writeLine(&sb, m.filterInput.View())
	}

	writeLine(&sb, sep)

	// Preview line — sits directly under the form so the command under
	// construction stays in visual lock-step with the field being edited.
	// The separator above is now the form/preview boundary; findings and
	// update notices move below the preview.
	cmd := m.buildCommand()
	if w > 0 {
		// render.Build already wrapped to m.width with `\` continuations;
		// defend against a pathological single line (no break points) by
		// truncating only per-line, never the whole multi-line block.
		lines := strings.Split(cmd, "\n")
		for i, ln := range lines {
			if runewidth.StringWidth(ln) > w {
				lines[i] = runewidth.Truncate(ln, w-1, "…")
			}
		}
		cmd = strings.Join(lines, "\n")
	}
	writeLine(&sb, previewStyle.Render(cmd))

	// Findings footer (spec §9) — now sits below the preview line.
	switch {
	case m.errorMsg != "":
		writeLine(&sb, errStyle.Render(m.errorMsg))
	case m.hintMsg != "":
		writeLine(&sb, hintStyle.Render(m.hintMsg))
	case focusedFullValue != "":
		// Grid mode shows a preview of the focused field's full value here so
		// truncated cells (`…`) don't hide information. Prefix with the field
		// name so this line can't be mistaken for a warning.
		label := ""
		if idx := m.fieldAt(m.cursor); idx >= 0 {
			label = m.fields[idx].Param.Name + " = "
		}
		writeLine(&sb, hintStyle.Render(label+focusedFullValue))
	default:
		// Show the currently-selected warning. Cycle with 'w'. The view only
		// surfaces one warning at a time so the footer stays one line; the
		// [N/total] indicator tells the user there are more.
		idx, total := m.currentWarning()
		if total > 0 {
			header := ""
			if total > 1 {
				header = fmt.Sprintf("[%d/%d] ", idx+1, total)
			}
			writeLine(&sb, hintStyle.Render(header+m.findings[idx].Message))
		}
	}
	if m.updateAvailable != "" {
		writeLine(&sb, hintStyle.Render(fmt.Sprintf("↑ azform %s available", m.updateAvailable)))
	}

	// Both buttons get a fixed 1-space inner padding so the word doesn't
	// shift horizontally when focus moves between them. Idle (unfocused)
	// uses a light-grey background so the buttons read as buttons; focused
	// uses the brighter green/yellow backgrounds.
	doneLabel := buttonIdleStyle.Render(" Done ")
	cancelLabel := buttonIdleStyle.Render(" Cancel ")
	if m.mode == FormModeDone {
		doneLabel = doneFocusStyle.Render(" Done ")
	}
	if m.mode == FormModeCancel {
		cancelLabel = cancelFocusStyle.Render(" Cancel ")
	}
	sb.WriteString("\n")
	writeLine(&sb, doneLabel+"  "+cancelLabel)

	// Bottom boundary: mirrors the separator above the field list so the
	// form's vertical extent is visible at a glance.
	writeLine(&sb, sep)

	return sb.String()
}

// renderHelp produces the cheatsheet overlay for FormModeHelp. Lists every
// key the widget understands (in FormModeList / Done / Cancel); edit/filter
// modes follow standard text-input conventions and aren't enumerated here.
// Any keypress dismisses the overlay.
func (m Form) renderHelp() string {
	w := m.width
	if w == 0 {
		w = 80
	}
	sep := strings.Repeat("─", w)

	// keyCol is the rendered width of the left "Keys" column; descCol
	// receives whatever remains. Picked to be wide enough for the longest
	// key combo we list (e.g. "tab / shift+tab").
	const keyCol = 22
	row := func(keys, desc string) string {
		keysPad := keys
		if len(keysPad) < keyCol {
			keysPad += strings.Repeat(" ", keyCol-len(keysPad))
		}
		return "  " + headerStyle.Render(keysPad) + desc
	}

	var sb strings.Builder
	writeLine(&sb, headerStyle.Render("azform — keyboard shortcuts"))
	writeLine(&sb, sep)

	sections := []struct {
		title string
		rows  [][2]string
	}{
		{
			title: "Navigation",
			rows: [][2]string{
				{"↑ / k", "move cursor up"},
				{"↓ / j", "move cursor down"},
				{"← / h", "move cursor left (grid)"},
				{"→ / l", "move cursor right (grid)"},
			},
		},
		{
			title: "Editing",
			rows: [][2]string{
				{"enter", "edit field / open enum popup / confirm"},
				{"space", "toggle optional field on/off (required fields show a hint)"},
				{"ctrl+g", "insert variable reference ($NAME) at cursor (edit mode)"},
				{"esc", "close popup / cancel edit"},
				{"/", "fuzzy filter visible fields"},
				{"g", "toggle Global Arguments section"},
			},
		},
		{
			title: "Variables",
			rows: [][2]string{
				{"v", "toggle var/literal mode (env-sourced fields only)"},
				{"d", "declare current var for the session"},
			},
		},
		{
			title: "Validation",
			rows: [][2]string{
				{"w", "cycle through non-blocking warnings in the footer"},
			},
		},
		{
			title: "Confirm / cancel",
			rows: [][2]string{
				{"tab", "focus Done button"},
				{"shift+tab", "focus Cancel button"},
				{"enter", "confirm focused button"},
				{"esc", "return to list from Done / Cancel"},
				{"q", "cancel from list"},
			},
		},
		{
			title: "Help",
			rows: [][2]string{
				{"? / f1", "show this overlay"},
				{"any key", "dismiss"},
			},
		},
	}
	for _, s := range sections {
		writeLine(&sb, "")
		writeLine(&sb, hintStyle.Render(s.title))
		for _, r := range s.rows {
			writeLine(&sb, row(r[0], r[1]))
		}
	}

	writeLine(&sb, "")
	writeLine(&sb, hintStyle.Render("Press any key to dismiss."))
	return sb.String()
}

// writeLine appends s plus a trailing newline without an intermediate string
// concatenation allocation.
func writeLine(sb *strings.Builder, s string) {
	sb.WriteString(s)
	sb.WriteByte('\n')
}

// nameColumnWidth returns the padding width for the flag-name column so the
// value column lines up vertically. It's the width of the widest flag name
// among currently-visible fields (required + all rendered optional), with a
// minimum of minNameCol so short-named forms don't look cramped.
func (m *Form) nameColumnWidth() int {
	const minNameCol = 24
	width := minNameCol
	widen := func(idx int) {
		if n := len(m.fields[idx].Param.Name); n > width {
			width = n
		}
	}
	for _, idx := range m.reqIndices {
		widen(idx)
	}
	for _, idx := range m.visible {
		f := &m.fields[idx]
		if f.Param.Required {
			continue
		}
		if f.Param.Global && !m.showGlobals && !f.Enabled {
			continue
		}
		widen(idx)
	}
	return width
}

func (m *Form) renderFieldSelected(idx int, selected bool, nameWidth int) string {
	f := &m.fields[idx]
	bullet := "○ "
	if f.Enabled {
		bullet = "● "
	}
	name := f.Param.Name
	if len(name) < nameWidth {
		name += strings.Repeat(" ", nameWidth-len(name))
	}
	var valDisplay string
	switch {
	case f.Param.IsSwitch():
		// Switches have no value column — bullet alone indicates on/off.
		// A bare bool's Value is the structural "true" set by the parser;
		// never render it. Editing a switch doesn't open a text input;
		// Space toggles it (handled by the keyswitch in handlers.go).
		valDisplay = ""
	case selected && m.mode == FormModeEdit:
		valDisplay = m.textInput.View()
	case f.Value == "":
		valDisplay = hintStyle.Render("—")
	case f.Mode == FieldModeVar:
		status := StatusOf(f.Value, m.sessionVars)
		valDisplay = status.Style().Render(f.DisplayValue())
	default:
		valDisplay = f.DisplayValue()
	}
	srcTag := ""
	if t := f.SourceTag(); t != "" {
		srcTag = "  " + srcTagStyle.Render(t)
	}
	// Required-row colouring replaces the old [req] tag: bullet+name is red
	// when the required field has no value, green when it does. The styling
	// is built with raw ANSI codes (not lipgloss.Style.Render) so we can use
	// a foreground-only reset (\x1b[39m) instead of a full reset (\x1b[0m).
	// A full reset drops the cursor-background set by selectedStyle; FG-only
	// reset preserves it, so the highlight spans the entire row instead of
	// only the bullet+name area.
	prefix := bullet + name + " "
	if f.Param.Required {
		// Match the SGR codes lipgloss emits for reqOkStyle (color 10) and
		// reqMissingStyle (color 9) so tests that probe the literal escape
		// still match. Use SGR 92/91 (bright ANSI) rather than 38;5;10/9
		// because that's what lipgloss falls back to in the TrueColor profile
		// for these "xterm-256"-style codes.
		var fgSGR string
		if f.Enabled && f.Value != "" {
			fgSGR = "\x1b[92m"
		} else {
			fgSGR = "\x1b[91m"
		}
		prefix = fgSGR + bullet + name + "\x1b[39m" + " "
	}
	row := prefix + valDisplay + srcTag
	// Lazy fetch state indicators (spec §6.1).
	switch {
	case f.FetchState == FetchLoading && f.FetchSpinnerShow:
		row += "  " + m.fieldSpinner.View()
	case f.FetchState == FetchError && f.FetchError != "":
		row += "  " + errStyle.Render(f.FetchError)
	case f.FetchState == FetchLoaded && len(f.FetchedChoices) > 0:
		row += "  " + hintStyle.Render(fmt.Sprintf("(%d options)", len(f.FetchedChoices)))
	}
	if selected {
		// Span the full form width. Width is applied via a style copy rather
		// than by appending spaces: lipgloss trims trailing whitespace from
		// styled strings, so padded spaces would silently disappear for rows
		// ending in a styled segment (— placeholder, $VAR values, …).
		if m.width > 0 {
			return selectedStyle.Width(m.width).Render(row)
		}
		return selectedStyle.Render(row)
	}
	return row
}

// currentWarning returns the index of the warning currently shown in the
// footer (clamped into range) and the total warning count. Called from
// View() and the 'w' key handler.
func (m *Form) currentWarning() (idx, total int) {
	warnings := m.warningFindings()
	total = len(warnings)
	if total == 0 {
		return 0, 0
	}
	if m.warningIdx < 0 || m.warningIdx >= total {
		m.warningIdx = 0
	}
	return m.warningIdx, total
}

// warningFindings returns findings whose severity is Warning (blockers have
// their own errorMsg slot in the footer).
func (m *Form) warningFindings() []validate.Finding {
	var out []validate.Finding
	for _, f := range m.findings {
		if f.Severity == validate.SeverityWarning {
			out = append(out, f)
		}
	}
	return out
}

func (m *Form) buildCommand() string {
	var fvs []render.FieldValue
	for _, f := range m.fields {
		// Skip globals unless the user explicitly enabled them — an enabled
		// global (e.g. --output=table) is intentional and belongs in output.
		if f.Param.Global && !f.Enabled {
			continue
		}
		val, isVar := f.ToRenderValue()
		fvs = append(fvs, render.FieldValue{
			Name:     f.Param.Name,
			Value:    val,
			IsVar:    isVar,
			IsSwitch: f.Param.IsSwitch(),
			Enabled:  f.Enabled,
		})
	}
	return render.Build(render.Command{
		Path:   m.command,
		Fields: fvs,
		Width:  m.width,
	})
}

// collectFieldValues returns a snapshot of the current field state for
// persisting as a draft on cancel. The boolean map captures fields the
// user has explicitly toggled off — without this, a binding-applied
// field the user disabled would reappear enabled on the next reopen.
func (m *Form) collectFieldValues() (map[string]string, map[string]bool) {
	values := make(map[string]string, len(m.fields))
	disabled := make(map[string]bool)
	for _, f := range m.fields {
		if f.Value != "" {
			values[f.Param.Name] = f.Value
		}
		if !f.Enabled {
			disabled[f.Param.Name] = true
		}
	}
	return values, disabled
}

// renderGrid assembles a multi-line grid string from column-partitioned
// field indices. Returns the body, the Y-row of the focused cell (or -1),
// the X-column of the focused cell (or -1), and the untruncated value of
// the focused field (or "").
//
// Cell layout per row: "● name(padded) [req] value(truncated)"; columns are
// separated by gridCellGap spaces. Blank cells in shorter columns get
// padded to the same width so trailing columns stay aligned.
func (m *Form) renderGrid(roCols [][]int, globalsCol []int) (body string, cursorRow, focusedCol int, focusedFullValue string) {
	// Combine columns for uniform iteration.
	all := append([][]int(nil), roCols...)
	if len(globalsCol) > 0 {
		all = append(all, globalsCol)
	}
	if len(all) == 0 {
		return "", -1, -1, ""
	}

	// Per-column name width so long-named columns don't waste space in short-named ones.
	nameWidths := make([]int, len(all))
	for c, col := range all {
		w := gridMinNameCol
		for _, idx := range col {
			if n := len(m.fields[idx].Param.Name); n > w {
				w = n
			}
		}
		nameWidths[c] = w
	}

	cellWidths := make([]int, len(all))
	for c, nw := range nameWidths {
		cellWidths[c] = 2 + nw + 1 + gridValueBudget
	}

	maxH := 0
	for _, col := range all {
		if len(col) > maxH {
			maxH = len(col)
		}
	}

	focusIdx := m.fieldAt(m.cursor)
	cursorRow = -1
	focusedCol = -1

	// Count hidden globals so we can emit a "press g" hint. Mirrors the
	// fit-vs-toggle logic in gridLayout: if all globals fit vertically, none
	// are hidden — no hint needed.
	hiddenGlobals := 0
	if !m.showGlobals {
		totalGlobals := 0
		for _, idx := range m.visible {
			if m.fields[idx].Param.Global {
				totalGlobals++
			}
		}
		availableRows := 20
		if m.vpReady && m.vp.Height > 0 {
			availableRows = m.vp.Height
		}
		if totalGlobals > availableRows {
			for _, idx := range m.visible {
				f := &m.fields[idx]
				if f.Param.Global && !f.Enabled {
					hiddenGlobals++
				}
			}
		}
	}

	var sb strings.Builder
	for row := 0; row < maxH; row++ {
		var line strings.Builder
		for c, col := range all {
			if row < len(col) {
				idx := col[row]
				cell := m.renderGridCell(idx, idx == focusIdx, nameWidths[c])
				line.WriteString(cell)
				// Pad any short cell to the fixed cell width so columns align.
				actualW := runewidth.StringWidth(stripANSI(cell))
				if actualW < cellWidths[c] {
					line.WriteString(strings.Repeat(" ", cellWidths[c]-actualW))
				}
				if idx == focusIdx {
					cursorRow = row
					focusedCol = c
					focusedFullValue = m.fields[idx].DisplayValue()
					// Suppress footer preview when the value already fits.
					if runewidth.StringWidth(focusedFullValue) <= gridValueBudget {
						focusedFullValue = ""
					}
				}
			} else {
				line.WriteString(strings.Repeat(" ", cellWidths[c]))
			}
			if c < len(all)-1 {
				line.WriteString(strings.Repeat(" ", gridCellGap))
			}
		}
		sb.WriteString(line.String())
		sb.WriteByte('\n')
	}

	// Hint: when globals were auto-hidden because they don't fit vertically,
	// let the user know 'g' will reveal them.
	if hiddenGlobals > 0 {
		if len(globalsCol) > 0 {
			sb.WriteString(hintStyle.Render(fmt.Sprintf("  (press g to show %d more global argument(s))", hiddenGlobals)))
		} else {
			sb.WriteString(hintStyle.Render(fmt.Sprintf("  (press g to show %d global argument(s))", hiddenGlobals)))
		}
		sb.WriteByte('\n')
	}

	return sb.String(), cursorRow, focusedCol, focusedFullValue
}

// spliceEnumOverlay inserts the enum popup as an overlay on top of the
// grid body, anchored at the focused cell's column and one row below
// the focused cell. Returns the patched lines. Lines beneath the
// overlay are replaced (not preserved) — the popup is transient and
// the grid recovers fully on close.
//
// popupWidth caps at the cell's left-edge-to-terminal-right span so
// the popup never wraps across rows. height is capped at the rows
// available below the focused cell so the overlay never bleeds into
// the separator / preview region. When there's no room below, the
// spliceEnumOverlay inserts the enum popup as an overlay on top of the
// grid body, anchored at the focused cell's column and one row below
// the focused cell. Returns the patched lines. Lines beneath the
// overlay are replaced (not preserved) — the popup is transient and
// the grid recovers fully on close.
//
// popupWidth caps at the cell's left-edge-to-terminal-right span so
// the popup never wraps across rows. height is capped at the rows
// available below the focused cell so the overlay never bleeds into
// the separator / preview region. When there's no room below, the
// overlay anchors above the focused cell instead.
func (m *Form) spliceEnumOverlay(lines []string, focusedRow, focusedCol int, cellWidths []int) []string {
	return m.spliceOverlay(lines, focusedRow, focusedCol, cellWidths, m.enumPop.choices, m.enumPop.cursor)
}

// spliceVarOverlay is the variable-picker's twin of spliceEnumOverlay.
// Both pickers share buildPopup so any tweak to placement propagates to
// both, but the var picker lays its entries out across N columns
// (side-by-side) instead of a single tall list, so the popup fits on
// the same screen as the form body for the typical 30–50 vars a shell
// exports.
func (m *Form) spliceVarOverlay(lines []string, focusedRow, focusedCol int, cellWidths []int) []string {
	if len(lines) == 0 || focusedRow < 0 {
		return lines
	}
	visibleIdx := focusedRow - m.vp.YOffset
	if visibleIdx < 0 || visibleIdx >= len(lines) {
		return lines
	}

	popup := m.buildVarPickerBox()
	if len(popup) == 0 {
		return lines
	}
	popupHeight := len(popup)

	belowSpace := len(lines) - (visibleIdx + 1)
	placeBelow := belowSpace >= popupHeight

	insertAt := visibleIdx + 1
	if !placeBelow {
		insertAt = visibleIdx - popupHeight
	}
	if insertAt < 0 {
		insertAt = 0
	}
	if insertAt > len(lines) {
		insertAt = len(lines)
	}
	if room := len(lines) - insertAt; len(popup) > room {
		popup = popup[:room]
	}
	head := lines[:insertAt]
	tail := []string{}
	if insertAt < len(lines) {
		tail = lines[insertAt:]
	}
	out := make([]string, 0, len(head)+len(popup)+len(tail))
	out = append(out, head...)
	out = append(out, popup...)
	if len(tail) > 0 {
		out = append(out, tail...)
	}
	return out
}

// buildVarPickerBox renders the unified bordered var-picker popup as a
// slice of lines. The box spans the full terminal width, has a fixed
// interior height of pickerRows, and shows column-major variable
// entries with a horizontal scroll indicator when overflowing.
func (m *Form) buildVarPickerBox() []string {
	w := m.width
	if w < 4 {
		w = 4
	}
	// Ensure the picker geometry matches the current terminal width.
	// Layout is a no-op when width hasn't changed enough to reflow.
	m.varPop.layout(w)

	innerWidth := w - 2
	if innerWidth < 1 {
		innerWidth = 1
	}
	cols := m.varPop.cols
	visibleCols := m.varPop.visibleCols
	offset := m.varPop.colOffset
	colW := m.varPop.colW
	cursorCol, cursorRow := m.varPop.colIdx, m.varPop.rowIdx

	// Top border with optional scroll indicator.
	top := buildTopBorder(innerWidth, cols, visibleCols, offset)

	// Reserved filter line above the grid. Shows `filter: <text>█` when
	// the user has typed anything, or the neutral `variables` label
	// when empty. Always reserved so the grid never jumps.
	filterLine := buildVarPickerFilterLine(m.varPop.Filter(), innerWidth)

	// Interior grid rows. When the filtered list is empty, render a
	// single centered `no matches` line and blank remaining rows.
	rows := make([]string, pickerRows)
	if cols == 0 {
		msgText := "no matches"
		if runewidth.StringWidth(msgText) > innerWidth {
			msgText = ansi.Truncate(msgText, innerWidth, "")
		}
		msgW := runewidth.StringWidth(msgText)
		leftPad := (innerWidth - msgW) / 2
		if leftPad < 0 {
			leftPad = 0
		}
		rightPad := innerWidth - leftPad - msgW
		if rightPad < 0 {
			rightPad = 0
		}
		midRow := pickerRows / 2
		blank := "│" + strings.Repeat(" ", innerWidth) + "│"
		for r := 0; r < pickerRows; r++ {
			if r == midRow {
				rows[r] = "│" + strings.Repeat(" ", leftPad) + msgText + strings.Repeat(" ", rightPad) + "│"
			} else {
				rows[r] = blank
			}
		}
		out := make([]string, 0, pickerRows+3)
		out = append(out, top, filterLine)
		out = append(out, rows...)
		out = append(out, "└"+strings.Repeat("─", innerWidth)+"┘")
		return out
	}
	for r := 0; r < pickerRows; r++ {
		var sb strings.Builder
		for c := 0; c < visibleCols; c++ {
			globalCol := offset + c
			entries := m.varPop.colEntries(globalCol)
			var cell string
			if r < len(entries) {
				name := entries[r]
				prefix := "  "
				isCursor := globalCol == cursorCol && r == cursorRow
				if isCursor {
					prefix = "▶ "
				}
				// Truncate name to colW - 2 (prefix width).
				maxName := colW - 2
				display := name
				if runewidth.StringWidth(display) > maxName {
					display = runewidth.Truncate(display, maxName-1, "…")
				}
				used := runewidth.StringWidth(prefix + display)
				if pad := colW - used; pad > 0 {
					display += strings.Repeat(" ", pad)
				}
				if isCursor {
					cell = enumCursorStyle.Render(prefix + display)
				} else {
					cell = prefix + display
				}
			} else {
				cell = strings.Repeat(" ", colW)
			}
			sb.WriteString(cell)
			if c < visibleCols-1 {
				sb.WriteString(strings.Repeat(" ", pickColGap))
			}
		}
		// Pad interior line to innerWidth.
		lineW := lipgloss.Width(sb.String())
		if pad := innerWidth - lineW; pad > 0 {
			sb.WriteString(strings.Repeat(" ", pad))
		}
		// Truncate if too long.
		line := sb.String()
		if lipgloss.Width(line) > innerWidth {
			line = ansi.Truncate(line, innerWidth, "")
		}
		rows[r] = "│" + line + "│"
	}

	bot := "└" + strings.Repeat("─", innerWidth) + "┘"

	out := make([]string, 0, pickerRows+3)
	out = append(out, top, filterLine)
	out = append(out, rows...)
	out = append(out, bot)
	return out
}

// buildVarPickerFilterLine renders the reserved filter-status row that
// sits between the top border and the entry grid. When filter is
// non-empty it shows `filter: <text>█`; when empty it shows the neutral
// `variables` label. The line is always present so the grid does not
// vertically jump when the user starts typing.
func buildVarPickerFilterLine(filter string, innerWidth int) string {
	if innerWidth < 1 {
		innerWidth = 1
	}
	var content string
	if filter == "" {
		content = " variables"
	} else {
		content = " filter: " + filter + "█"
	}
	if runewidth.StringWidth(content) > innerWidth {
		content = ansi.Truncate(content, innerWidth, "")
	}
	if pad := innerWidth - runewidth.StringWidth(content); pad > 0 {
		content += strings.Repeat(" ", pad)
	}
	return "│" + content + "│"
}

// buildTopBorder returns the top border of the var picker box. When the
// picker has more columns than fit on screen, a `─ N-M/T ─` indicator is
// centered on the border. innerWidth is the width between the corners.
func buildTopBorder(innerWidth, cols, visibleCols, offset int) string {
	if innerWidth < 1 {
		innerWidth = 1
	}
	if cols <= visibleCols || cols == 0 {
		return "┌" + strings.Repeat("─", innerWidth) + "┐"
	}
	first := offset + 1
	last := offset + visibleCols
	if last > cols {
		last = cols
	}
	label := fmt.Sprintf("─ %d-%d/%d ─", first, last, cols)
	labelW := runewidth.StringWidth(label)
	if labelW >= innerWidth {
		return "┌" + strings.Repeat("─", innerWidth) + "┐"
	}
	leftPad := (innerWidth - labelW) / 2
	rightPad := innerWidth - labelW - leftPad
	return "┌" + strings.Repeat("─", leftPad) + label + strings.Repeat("─", rightPad) + "┐"
}

// enumPopupHeight returns the number of terminal rows the enum popup
// will occupy for the given choices (top + visible items + bottom).
func enumPopupHeight(choices []string) int {
	n := len(choices)
	if n == 0 {
		return 0
	}
	if n > maxPopupItems {
		n = maxPopupItems
	}
	return n + 2
}

// maxPopupItems caps the visible choice rows in an enum popup. Longer
// lists slide a window around the cursor and mark the clipped side with
// a "↑"/"↓" glyph inside the top/bottom border.
const maxPopupItems = 7

// buildPopupLines returns the bordered popup block for one column of
// choices. The first and last lines are top/bottom borders; the
// interior lines are `│ label │` with `▶ ` prefix on the cursor row.
// Choices wider than the interior are truncated with `…`. When more
// than maxPopupItems choices are present, the visible slice slides to
// keep the cursor in view and clipped sides are marked in the border.
// Returns an empty slice when choices is empty (caller skips drawing).
func buildPopupLines(choices []string, cursor int) []string {
	n := len(choices)
	if n == 0 {
		return nil
	}
	// Width = longest choice + cursor + padding, floored at gridValueBudget
	// so single-character choices still get a comfortable popup. Matches
	// the original spliceOverlay's min-width floor.
	popupWidth := gridValueBudget
	for _, c := range choices {
		if w := runewidth.StringWidth(c) + 4; w > popupWidth {
			popupWidth = w
		}
	}
	interior := popupWidth - 2

	visible := n
	if visible > maxPopupItems {
		visible = maxPopupItems
	}
	start := 0
	if n > visible {
		start = cursor - visible/2
		if start < 0 {
			start = 0
		}
		if start+visible > n {
			start = n - visible
		}
	}
	end := start + visible
	clipTop := start > 0
	clipBot := end < n

	var out []string
	out = append(out, borderLine("┌", "┐", interior, clipTop, "↑"))
	for i := start; i < end; i++ {
		choice := choices[i]
		prefix := "  "
		styled := false
		if i == cursor {
			prefix = "▶ "
			styled = true
		}
		label := choice
		maxLabel := interior - 4 // "▶ "/"  " + 2 padding
		if runewidth.StringWidth(label) > maxLabel {
			label = runewidth.Truncate(label, maxLabel-1, "…")
		}
		used := runewidth.StringWidth(prefix + label)
		if pad := interior - used; pad > 0 {
			label += strings.Repeat(" ", pad)
		}
		var line strings.Builder
		line.WriteString("│")
		if styled {
			line.WriteString(enumCursorStyle.Render(prefix + label))
		} else {
			line.WriteString(prefix)
			line.WriteString(label)
		}
		line.WriteString("│")
		out = append(out, line.String())
	}
	out = append(out, borderLine("└", "┘", interior, clipBot, "↓"))
	return out
}

// borderLine builds a horizontal border ("┌────┐" style). When clipped
// is true, the middle of the border shows `glyph` to signal off-screen
// choices in that direction.
func borderLine(left, right string, interior int, clipped bool, glyph string) string {
	if !clipped || interior < 3 {
		var b strings.Builder
		b.WriteString(left)
		b.WriteString(strings.Repeat("─", interior))
		b.WriteString(right)
		return b.String()
	}
	mid := interior / 2
	var b strings.Builder
	b.WriteString(left)
	b.WriteString(strings.Repeat("─", mid))
	b.WriteString(glyph)
	b.WriteString(strings.Repeat("─", interior-mid-1))
	b.WriteString(right)
	return b.String()
}

func (m *Form) spliceOverlay(lines []string, focusedRow, focusedCol int, cellWidths []int, choices []string, cursor int) []string {
	if len(lines) == 0 || focusedRow < 0 || focusedCol < 0 || focusedCol >= len(cellWidths) {
		return lines
	}

	// Anchor x = sum of column widths up to focusedCol (plus the inter-column gap).
	xAnchor := 0
	for c := 0; c < focusedCol; c++ {
		xAnchor += cellWidths[c] + gridCellGap
	}

	// Anchor y = the focused row in the viewport.
	visibleIdx := focusedRow - m.vp.YOffset
	if visibleIdx < 0 || visibleIdx >= len(lines) {
		return lines
	}

	if len(choices) == 0 {
		return lines
	}

	// Cap width at the cell's left-edge-to-terminal-right span so the
	// popup never wraps. Floor at gridValueBudget so single-character
	// choices still render legibly.
	popupWidth := gridValueBudget
	for _, c := range choices {
		if w := runewidth.StringWidth(c) + 4; w > popupWidth {
			popupWidth = w
		}
	}
	if xAnchor+popupWidth > m.width {
		if m.width >= popupWidth {
			xAnchor = m.width - popupWidth
		} else {
			xAnchor = 0
		}
	}

	popupLines := buildPopupLines(choices, cursor)
	if len(popupLines) == 0 {
		return lines
	}

	belowSpace := len(lines) - (visibleIdx + 1)
	aboveSpace := visibleIdx

	// Prefer placing below (natural reading order); flip to above only
	// when the popup fully fits there and doesn't below. If neither side
	// has full room, place on the side with more space and clip the
	// popup at the viewport edge.
	fitsBelow := belowSpace >= len(popupLines)
	fitsAbove := aboveSpace >= len(popupLines)
	var startRow int
	switch {
	case fitsBelow:
		startRow = visibleIdx + 1
	case fitsAbove:
		startRow = visibleIdx - len(popupLines)
	default:
		if belowSpace >= aboveSpace {
			startRow = visibleIdx + 1
		} else {
			startRow = visibleIdx - len(popupLines)
			if startRow < 0 {
				startRow = 0
			}
		}
	}

	// Composite the popup as a character-level overlay: each popup line
	// is spliced into the base line at xAnchor, keeping cells outside
	// the popup's footprint intact. This preserves adjacent grid columns
	// (e.g. the required column when the popup anchors under an
	// optional/globals column) that a whole-line replacement would wipe.
	out := make([]string, len(lines))
	copy(out, lines)
	for i, pl := range popupLines {
		r := startRow + i
		if r < 0 || r >= len(out) {
			continue
		}
		out[r] = overlayAt(out[r], pl, xAnchor)
	}
	return out
}

// overlayAt splices `overlay` into `base` starting at visible column x,
// preserving cells before x and after x+width(overlay). ANSI escapes in
// base are honored; the overlay's own styling is included verbatim.
func overlayAt(base, overlay string, x int) string {
	overlayW := ansi.StringWidth(overlay)
	left := ansi.Truncate(base, x, "")
	if lw := ansi.StringWidth(left); lw < x {
		left += strings.Repeat(" ", x-lw)
	}
	var right string
	if ansi.StringWidth(base) > x+overlayW {
		right = ansi.TruncateLeft(base, x+overlayW, "")
	}
	return left + overlay + right
}

// renderGridCell produces one cell for the grid: bullet + padded name +
// optional [req] tag + value (truncated to gridValueBudget with …). The whole
// cell is wrapped with selectedStyle when selected. Fetch-status indicators
// and source tags are dropped for space.
func (m *Form) renderGridCell(idx int, selected bool, nameWidth int) string {
	f := &m.fields[idx]
	bullet := "○ "
	if f.Enabled {
		bullet = "● "
	}
	name := f.Param.Name
	if len(name) < nameWidth {
		name += strings.Repeat(" ", nameWidth-len(name))
	}
	// valueBudget is gridValueBudget, but shrinks when the focused cell
	// holds a text input — the textinput owns its own width and any
	// fixed-budget truncation here would be redundant.
	valueBudget := gridValueBudget
	var valDisplay string
	switch {
	case f.Param.IsSwitch():
		// No value column — bullet alone indicates on/off.
		valDisplay = ""
	case selected && m.mode == FormModeEdit && !f.Param.IsSwitch():
		// Text edit in-place: replace the value column with the textinput.
		// bubbles/textinput's View() can render slightly wider than its
		// Width (cursor cell + styling), which would push the row past
		// cellWidth and cause lipgloss to wrap the overflow onto the
		// next terminal line — breaking the grid layout. Truncate to
		// the value budget so the cell stays on one line.
		valDisplay = ansi.Truncate(m.textInput.View(), valueBudget, "")
	case f.Value == "":
		valDisplay = hintStyle.Render("—")
	case f.Mode == FieldModeVar:
		status := StatusOf(f.Value, m.sessionVars)
		valDisplay = status.Style().Render(runewidth.Truncate(f.DisplayValue(), valueBudget-1, "…"))
	default:
		valDisplay = runewidth.Truncate(f.DisplayValue(), valueBudget-1, "…")
	}
	prefix := bullet + name + " "
	if f.Param.Required {
		var fgSGR string
		if f.Enabled && f.Value != "" {
			fgSGR = "\x1b[92m"
		} else {
			fgSGR = "\x1b[91m"
		}
		prefix = fgSGR + bullet + name + "\x1b[39m" + " "
	}
	row := prefix + valDisplay
	if selected {
		// Span the column's cell width (mirrors renderGrid's cellWidths).
		// Width is applied via a style copy rather than by appending spaces:
		// lipgloss trims trailing whitespace from styled strings, so padded
		// spaces would silently disappear when the value ends in a styled
		// segment (— placeholder, $VAR values, …).
		cellWidth := 2 + nameWidth + 1 + gridValueBudget
		return selectedStyle.Width(cellWidth).MaxWidth(cellWidth).Render(row)
	}
	return row
}

// stripANSI removes ANSI escape sequences so we can measure the visual width
// of a styled string. Handles CSI (`\x1b[…m`) and OSC (`\x1b]…\x07` or `\x1b\\`) forms.
func stripANSI(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) {
			if s[i+1] == '[' {
				// CSI: consume until final byte in 0x40–0x7E.
				j := i + 2
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7E) {
					j++
				}
				if j < len(s) {
					j++
				}
				i = j
				continue
			}
			if s[i+1] == ']' {
				// OSC: consume until BEL or ESC-\\.
				j := i + 2
				for j < len(s) && s[j] != 0x07 {
					if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				if j < len(s) && s[j] == 0x07 {
					j++
				}
				i = j
				continue
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}
