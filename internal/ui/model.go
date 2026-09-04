package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/someson/azform/internal/debug"
	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/shell"
	"github.com/someson/azform/internal/state"
	"github.com/someson/azform/internal/update"
	"github.com/someson/azform/internal/validate"
	"github.com/someson/azform/internal/vars"
)

// FormMode tracks which interaction layer has keyboard focus.
type FormMode int

const (
	FormModeList    FormMode = iota // navigating the field list
	FormModeEdit                    // text input inside a field
	FormModeEnum                    // enum popup open
	FormModeFilter                  // / fuzzy filter input active
	FormModeDone                    // Tab → Done button focused
	FormModeCancel                  // Tab → Cancel button focused
	FormModeHelp                    // ? cheatsheet overlay open
	FormModeVarPick                 // Ctrl+V → variable picker popup open
)

// LoadState tracks async metadata fetch.
type LoadState int

const (
	LoadStateLoading LoadState = iota
	LoadStateLoaded
	LoadStateError
)

// MetadataLoadedMsg is sent by the async loader when metadata is ready.
// Exported so tests can inject it directly.
type MetadataLoadedMsg struct {
	Params  []metadata.Parameter
	Summary string
	Stale   bool
	Health  metadata.ParseHealth
}

type metadataErrorMsg struct{ err error }

// Styles
var (
	headerStyle      = lipgloss.NewStyle().Bold(true)
	selectedStyle    = lipgloss.NewStyle().Background(lipgloss.Color("237"))
	srcTagStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	hintStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	previewStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	warnStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	doneFocusStyle   = lipgloss.NewStyle().Background(lipgloss.Color("10")).Foreground(lipgloss.Color("0")).Bold(true) // green bg — focused Done
	cancelFocusStyle = lipgloss.NewStyle().Background(lipgloss.Color("11")).Foreground(lipgloss.Color("0")).Bold(true) // yellow bg — focused Cancel
	// Idle button styles: light grey background so they always read as
	// buttons (not loose text). Same padding as the focused variants so
	// the words don't shift horizontally when the focus moves.
	buttonIdleStyle = lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("15"))
)

const (
	heightRatio  = 0.7
	hintDuration = 2 * time.Second

	// Lazy fetch thresholds (spec §6.1 Field fetch state).
	fetchSpinnerDelay  = 150 * time.Millisecond
	fetchSlowThreshold = 3 * time.Second
	fetchCancelOffer   = 10 * time.Second
	fetchSlowHint      = "az is slow to start — this result will be cached"
	fetchCancelHint    = "fetch taking too long — press Esc to cancel"
	fetchCancelledHint = "fetch cancelled — enter value manually"
)

// Form is the bubbletea model for the azform parameter form.
type Form struct {
	command  string
	outPath  string
	stateDir string
	version  string
	cache    *metadata.Cache

	loadState LoadState
	loadErr   string
	summary   string

	fields     []Field
	visible    []int // filtered subset of field indices
	reqIndices []int // required non-global field indices (always pinned)
	cursor     int

	vp      viewport.Model
	vpReady bool

	textInput textinput.Model
	editIdx   int

	enumPop EnumModel
	enumIdx int

	varPop        VarPickerModel
	varEditCursor int // textinput cursor position saved when picker opens; restored on insert/cancel

	filterInput textinput.Model
	filterQuery string

	mode FormMode

	width  int
	height int

	spin spinner.Model

	draftStore    *state.DraftStore
	draftRestored bool

	// showGlobals controls whether Azure CLI "Global Arguments" (--output,
	// --query, --subscription, --verbose, --debug, etc.) are rendered in the
	// optional list. Toggle with the 'g' key. Enabled globals always appear
	// in the built command regardless of this flag.
	showGlobals bool

	staleWarn string
	quitting  bool
	result    string
	src       Sources

	findings        []validate.Finding
	warningIdx      int // index of the currently-shown warning in the footer; cycle with 'w'
	sessionVars     map[string]bool
	errorMsg        string
	hintMsg         string // transient footer feedback (spec §6.6: Space-on-required)
	hintActive      bool   // a tea.Tick is pending to clear hintMsg
	declaredVars    []DeclaredVar
	declaring       bool
	updateAvailable string

	// Lazy field-fetch state (spec §6.1).
	fieldSpinner   spinner.Model // per-field inline spinner
	slowFetchIdx   int           // -1 = no slow-fetch hint active; else field idx
	cancelFetchIdx int           // -1 = no cancel-offer hint active; else field idx
}

// HintClearMsg clears the transient footer hint. Dispatched by the
// tea.Tick scheduled when Space is pressed on a required parameter.
// Exported so tests can drive the timer deterministically.
type HintClearMsg struct{}

// FieldSpinnerShowMsg flips the per-field inline spinner on once the
// 150 ms visibility threshold has elapsed (spec §6.1).
type FieldSpinnerShowMsg struct{ FieldIdx int }

// FieldFetchSlowMsg escalates the footer hint at 3 s: "az is slow to
// start — this result will be cached". No-op if the fetch has already
// completed by the time the tick fires.
type FieldFetchSlowMsg struct{ FieldIdx int }

// FieldFetchOfferCancelMsg at 10 s: footer hint reads "fetch taking too
// long — press Esc to cancel". Pressing Esc during this window cancels
// the fetch and frees the field for manual input.
type FieldFetchOfferCancelMsg struct{ FieldIdx int }

// DeclaredVar records a var the user explicitly assigned in the form (spec §8.4
// option 3). The result wrapper prepends these declarations.
type DeclaredVar struct {
	Name  string
	Value string
}

// Sources bundles all pre-fill inputs the form should consume on metadata
// load (spec §8.5). Each field may be nil/empty — the form degrades gracefully.
type Sources struct {
	Buffer        shell.RawBuffer
	Vars          []vars.Variable
	AzureDefaults []vars.Variable
	Engine        *validate.Engine
	SessionVars   []string
	Bindings      *state.BindingsStore
	UpdateCheck   tea.Cmd // background self-update check (M8, spec §14.4)
	Debug         *debug.Logger
}

// NewForm constructs a Form. cache may be nil (tests inject MetadataLoadedMsg directly).
func NewForm(command, outPath, stateDir, version string, cache *metadata.Cache) Form {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	fsp := spinner.New()
	fsp.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Placeholder = "value"

	fi := textinput.New()
	fi.Placeholder = "filter params…"
	fi.Prompt = "/ "

	return Form{
		command:        command,
		outPath:        outPath,
		stateDir:       stateDir,
		version:        version,
		cache:          cache,
		loadState:      LoadStateLoading,
		spin:           sp,
		fieldSpinner:   fsp,
		slowFetchIdx:   -1,
		cancelFetchIdx: -1,
		textInput:      ti,
		filterInput:    fi,
		draftStore:     state.NewDraftStore(stateDir),
	}
}

// NewFormWithSources constructs a Form with all pre-fill inputs. Any field of
// src may be zero — the form opens with what is available.
func NewFormWithSources(command, outPath, stateDir, version string, cache *metadata.Cache, src Sources) Form {
	f := NewForm(command, outPath, stateDir, version, cache)
	f.src = src
	return f
}

// NewFormWithBuffer constructs a Form with a pre-parsed shell buffer. It is
// retained for backward compatibility; prefer NewFormWithSources when adding
// new pre-fill sources.
func NewFormWithBuffer(command, outPath, stateDir, version string, cache *metadata.Cache, raw shell.RawBuffer) Form {
	return NewFormWithSources(command, outPath, stateDir, version, cache, Sources{Buffer: raw})
}

// Init satisfies tea.Model. Starts the spinner and kicks off metadata load.
func (m Form) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick}
	if m.cache != nil {
		cmds = append(cmds, m.fetchMetadata())
	}
	if m.src.UpdateCheck != nil {
		cmds = append(cmds, m.src.UpdateCheck)
	}
	return tea.Batch(cmds...)
}

// groupNotCommandErr produces a helpful error when the user pointed azform
// at a namespace (e.g. "az group") instead of a leaf command. It lists the
// available subcommands and subgroups so the user knows what to type next.
func groupNotCommandErr(command string, group *metadata.GroupRecord) error {
	if group == nil {
		return fmt.Errorf("%q is a command group, not a command — pick a leaf command (e.g. %q create)", command, command)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%q is a command group, not a command.\n", command)
	if len(group.Commands) > 0 {
		sb.WriteString("\nAvailable commands:\n")
		for _, c := range group.Commands {
			fmt.Fprintf(&sb, "  az %s %s", command, c.Name)
			if c.Summary != "" {
				fmt.Fprintf(&sb, "  — %s", c.Summary)
			}
			sb.WriteByte('\n')
		}
	}
	if len(group.Subgroups) > 0 {
		sb.WriteString("\nSubgroups:\n")
		for _, g := range group.Subgroups {
			fmt.Fprintf(&sb, "  az %s %s\n", command, g.Name)
		}
	}
	return fmt.Errorf("%s", sb.String())
}

func (m Form) fetchMetadata() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := m.cache.Resolve(ctx, m.command)
		if err != nil {
			return metadataErrorMsg{err}
		}
		if result.Command == nil {
			return metadataErrorMsg{groupNotCommandErr(m.command, result.Group)}
		}
		return MetadataLoadedMsg{
			Params:  result.Command.Parameters,
			Summary: result.Command.Summary,
			Stale:   result.Stale,
			Health:  result.Command.ParseHealth,
		}
	}
}

// Update satisfies tea.Model. Dispatches Bubble Tea messages to the
// appropriate handler based on m.mode and the message type.
func (m Form) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case HintClearMsg:
		m.hintMsg = ""
		m.hintActive = false
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		if m.loadState == LoadStateLoading {
			m.spin, cmd = m.spin.Update(msg)
		}
		if anyFieldLoading(m.fields) {
			var fcmd tea.Cmd
			m.fieldSpinner, fcmd = m.fieldSpinner.Update(msg)
			if cmd == nil {
				cmd = fcmd
			} else {
				cmd = tea.Batch(cmd, fcmd)
			}
		}
		return m, cmd

	case FieldFetchedMsg:
		if msg.FieldIdx < 0 || msg.FieldIdx >= len(m.fields) {
			return m, nil
		}
		f := &m.fields[msg.FieldIdx]
		if f.FetchState != FetchLoading {
			// Stale completion (e.g. after Esc cancel) — ignore.
			return m, nil
		}
		if msg.Err != nil {
			f.FetchState = FetchError
			f.FetchError = msg.Err.Error()
		} else {
			f.FetchedChoices = msg.Choices
			f.FetchState = FetchLoaded
		}
		f.FetchSpinnerShow = false
		// Clear slow/offer hints if they were shown for this field.
		if m.slowFetchIdx == msg.FieldIdx {
			m.slowFetchIdx = -1
			m.hintMsg = ""
			m.hintActive = false
		}
		if m.cancelFetchIdx == msg.FieldIdx {
			m.cancelFetchIdx = -1
			m.hintMsg = ""
			m.hintActive = false
		}
		return m, nil

	case FieldSpinnerShowMsg:
		if msg.FieldIdx < 0 || msg.FieldIdx >= len(m.fields) {
			return m, nil
		}
		if f := &m.fields[msg.FieldIdx]; f.FetchState == FetchLoading {
			f.FetchSpinnerShow = true
			return m, m.fieldSpinner.Tick
		}
		return m, nil

	case FieldFetchSlowMsg:
		if msg.FieldIdx < 0 || msg.FieldIdx >= len(m.fields) {
			return m, nil
		}
		if f := &m.fields[msg.FieldIdx]; f.FetchState == FetchLoading {
			m.slowFetchIdx = msg.FieldIdx
			m.hintMsg = fetchSlowHint
			m.hintActive = true
		}
		return m, nil

	case FieldFetchOfferCancelMsg:
		if msg.FieldIdx < 0 || msg.FieldIdx >= len(m.fields) {
			return m, nil
		}
		if f := &m.fields[msg.FieldIdx]; f.FetchState == FetchLoading {
			m.cancelFetchIdx = msg.FieldIdx
			m.hintMsg = fetchCancelHint
			m.hintActive = true
		}
		return m, nil

	case MetadataLoadedMsg:
		return m.handleMetadataLoaded(msg)

	case update.AvailableMsg:
		m.updateAvailable = msg.Latest
		return m, nil

	case metadataErrorMsg:
		m.loadState = LoadStateError
		m.loadErr = msg.err.Error()
		return m, nil

	case EnumSelectedMsg:
		if m.mode == FormModeEnum {
			m.fields[m.enumIdx].Value = msg.Value
			m.fields[m.enumIdx].Enabled = true
			m.recomputeFindings(nil)
			m.mode = FormModeList
		}
		return m, nil

	case EnumCancelledMsg:
		if m.mode == FormModeEnum {
			m.mode = FormModeList
		}
		return m, nil

	case VarPickedMsg:
		// Insert `$NAME` at the saved textinput cursor position. The
		// textinput stays in edit mode so the user can keep typing.
		if m.mode == FormModeVarPick {
			insert := "$" + msg.Name
			m.textInput.SetValue(m.textInput.Value()[:m.varEditCursor] + insert + m.textInput.Value()[m.varEditCursor:])
			m.textInput.SetCursor(m.varEditCursor + len(insert))
			m.mode = FormModeEdit
		}
		return m, nil

	case VarPickerCancelledMsg:
		if m.mode == FormModeVarPick {
			m.mode = FormModeEdit
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Route to sub-components.
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
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	case FormModeFilter:
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// sessionVarNames returns the union of vars.SessionVars names and vars.Vars
// names available in the current shell.
// maybeFetchField promotes a field from Idle to Loading and schedules the
// three tea.Tick thresholds + the subprocess fetch. Returns nil when the
// field is not eligible (already loading/loaded, no ValuesFrom, metadata
// already supplied choices). Caller chains the returned cmd with whatever
// they were already returning.
func (m *Form) maybeFetchField(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.fields) {
		return nil
	}
	f := &m.fields[idx]
	if f.FetchState != FetchIdle {
		return nil
	}
	vf := f.Param.ValuesFrom
	if vf == nil || *vf == "" {
		return nil
	}
	if len(f.Param.Choices) > 0 {
		return nil
	}
	f.FetchState = FetchLoading
	f.FetchSpinnerShow = false
	f.FetchStartedAt = time.Now()
	f.FetchedChoices = nil
	f.FetchError = ""
	return tea.Batch(
		tea.Tick(fetchSpinnerDelay, func(time.Time) tea.Msg {
			return FieldSpinnerShowMsg{FieldIdx: idx}
		}),
		tea.Tick(fetchSlowThreshold, func(time.Time) tea.Msg {
			return FieldFetchSlowMsg{FieldIdx: idx}
		}),
		tea.Tick(fetchCancelOffer, func(time.Time) tea.Msg {
			return FieldFetchOfferCancelMsg{FieldIdx: idx}
		}),
		fetchField(idx, *vf),
	)
}

// anyFieldLoading reports whether any field is currently mid-fetch; used to
// decide whether spinner ticks should keep animating the inline spinner.
func anyFieldLoading(fs []Field) bool {
	for _, f := range fs {
		if f.FetchState == FetchLoading {
			return true
		}
	}
	return false
}

func (m *Form) sessionVarNames() map[string]bool {
	set := map[string]bool{}
	for _, n := range m.src.SessionVars {
		set[n] = true
	}
	for _, v := range m.src.Vars {
		set[v.Name] = true
	}
	return set
}

// recomputeFindings rebuilds m.findings using m.src.Engine. params may be nil
// (the engine will then operate against an empty command).
func (m *Form) recomputeFindings(params []metadata.Parameter) {
	if m.src.Engine == nil {
		m.findings = nil
		return
	}
	if params == nil {
		params = m.currentParams()
	}
	st := m.buildFormState(params)
	m.findings = m.src.Engine.Run(context.Background(), &metadata.Command{Command: m.command, Parameters: params}, st)
	if total := len(m.warningFindings()); total == 0 {
		m.warningIdx = 0
	} else if m.warningIdx >= total {
		m.warningIdx = 0
	}
}

// currentParams extracts the param list from the loaded fields (used after
// the metadata message has been processed).
func (m *Form) currentParams() []metadata.Parameter {
	out := make([]metadata.Parameter, 0, len(m.fields))
	for _, f := range m.fields {
		out = append(out, f.Param)
	}
	return out
}

// buildFormState snapshots the form into a validate.FormState for rule
// inspection. Rules are pure; this method does not mutate m.
func (m *Form) buildFormState(params []metadata.Parameter) *validate.FormState {
	values := map[string]string{}
	modes := map[string]validate.FieldMode{}
	enabled := map[string]bool{}
	usedVars := map[string]bool{}
	for _, f := range m.fields {
		if f.Param.Name == "" {
			continue
		}
		values[f.Param.Name] = f.Value
		modes[f.Param.Name] = f.Mode
		enabled[f.Param.Name] = f.Enabled
		if f.Mode == validate.FieldModeVar && len(f.Value) > 1 && f.Value[0] == '$' && f.Value[1] != '(' {
			// Walk every whitespace-separated token in the value so multi-var
			// lists (`$a1 $a2`) report both names to the undefinedVar rule.
			for _, tok := range strings.Fields(f.Value) {
				if len(tok) < 2 || tok[0] != '$' || tok[1] == '(' {
					continue
				}
				rest := tok[1:]
				if rest == "" {
					continue
				}
				if rest[0] == '{' && tok[len(tok)-1] == '}' {
					rest = rest[1 : len(rest)-1]
				}
				if rest != "" && isVarName(rest) {
					usedVars[rest] = true
				}
			}
		}
	}

	unknown := []string{}
	flagOcc := map[string]int{}
	for _, tok := range m.src.Buffer.FlagTokens {
		if tok.Kind != shell.TokWord {
			continue
		}
		if !strings.HasPrefix(tok.Value, "-") {
			continue
		}
		canonical := ""
		for _, p := range params {
			for _, n := range p.AllNames() {
				if n == tok.Value {
					canonical = p.Name
					break
				}
			}
			if canonical != "" {
				break
			}
		}
		if canonical != "" {
			flagOcc[canonical]++
		} else {
			unknown = append(unknown, tok.Value)
		}
	}

	// Use the parser's Positional list rather than scanning raw tokens:
	// flag values like `myAppGateway`, `$RG` are non-flag tokens that the
	// naive scan above would misclassify as positionals.
	//
	// Filter against current field values: since m.src.Buffer is
	// immutable but field values can be edited in the widget, a token
	// that matches a currently-set flag's value is really a flag-value
	// the tokenizer split off (e.g. from unbalanced quotes), not a real
	// unexpected positional. Dropping those keeps the recompute honest.
	positional := []string{}
	parsed := shell.MatchParams(m.src.Buffer, params)
	activeValues := map[string]bool{}
	for _, f := range m.fields {
		if f.Enabled && f.Value != "" {
			activeValues[f.Value] = true
			for _, tok := range strings.Fields(f.Value) {
				activeValues[tok] = true
			}
		}
	}
	for _, t := range parsed.Positional {
		if activeValues[t.Value] {
			continue
		}
		positional = append(positional, t.Value)
	}

	used := make([]string, 0, len(usedVars))
	for n := range usedVars {
		used = append(used, n)
	}
	return &validate.FormState{
		Params:          params,
		Values:          values,
		Modes:           modes,
		Enabled:         enabled,
		UnknownFlags:    unknown,
		Positional:      positional,
		SessionVars:     m.sessionVars,
		UsedVarNames:    used,
		FlagOccurrences: flagOcc,
	}
}

// isVarName returns true if s is a valid POSIX shell variable name
// (letter/underscore, then letters/digits/underscores).
func isVarName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func (m *Form) rebuildVisible() {
	m.visible = FilterFields(m.fields, m.filterQuery)
	// If the currently-focused field is still visible after re-filtering,
	// keep the cursor on it — the user expects /-filter to narrow the list
	// around the field they had focus on, not to jump the highlight to a
	// different row. Otherwise (the field was filtered out) the old code
	// clamped to len-1, which lands on the last match — confusing if the
	// user was several rows past where the highlight ends up. Reset to 0
	// so the first match is always selected.
	prev := m.fieldAt(m.cursor)
	if prev < 0 {
		m.cursor = 0
	}
}

func (m *Form) fieldAt(pos int) int {
	if pos < 0 || pos >= len(m.visible) {
		return -1
	}
	return m.visible[pos]
}

func (m *Form) updateLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	headerLines := 3
	requiredLines := len(m.reqIndices)
	footerLines := 4
	available := int(float64(m.height)*heightRatio) - headerLines - requiredLines - footerLines
	if available < 3 {
		available = 3
	}
	if !m.vpReady {
		m.vp = viewport.New(m.width, available)
		m.vpReady = true
	} else {
		m.vp.Width = m.width
		m.vp.Height = available
	}
}

// gridValueBudget is the target width in runes for a value cell in the
// multicolumn grid. Values longer than this get runewidth.Truncated with …
// and the focused row shows the full value in the footer.
const gridValueBudget = 24
const gridCellGap = 2
const gridMinNameCol = 12
const gridSplitThreshold = 10 // > this many Req+Opt fields → split into 2 cols
const gridMaxCols = 3

// widestName returns the widest visible flag name across all m.visible fields
// that would render in the grid (respecting the globals-hide rule). Used to
// size grid cells uniformly.
func (m *Form) widestName() int {
	w := gridMinNameCol
	for _, idx := range m.visible {
		f := &m.fields[idx]
		if f.Param.Global && !m.showGlobals && !f.Enabled {
			continue
		}
		if n := len(f.Param.Name); n > w {
			w = n
		}
	}
	return w
}

// gridCellWidth returns the per-cell width (in runes) for a multicolumn
// render. Returns 0 when the terminal is too narrow to fit at least two
// cells side-by-side — caller falls back to single-column layout.
func (m *Form) gridCellWidth() int {
	if m.width == 0 {
		return 0
	}
	cell := 2 + m.widestName() + 1 + gridValueBudget // bullet + name + space + value
	if m.width < 2*cell+gridCellGap {
		return 0
	}
	return cell
}

// gridLayout partitions m.visible into columns for a multicolumn render.
//
// Returns:
//   - roCols: Req+Opt field indices split across 1 or 2 columns
//     (required-first order preserved).
//   - globalsCol: global field indices for the last column; empty when
//     showGlobals is off and no globals are enabled AND there are no globals
//     to hint about.
//   - cols: total column count (1, 2, or 3). Callers use cols == 1 to fall
//     back to the single-column render path.
//
// Grid is disabled (returns cols == 1, nil, nil) when the terminal is too
// narrow or when the caller shouldn't grid (edit/enum modes handled at the
// view layer, not here).
func (m *Form) gridLayout() (roCols [][]int, globalsCol []int, cols int) {
	if m.gridCellWidth() == 0 {
		return nil, nil, 1
	}

	// Count total globals to decide if they all fit vertically. When they do,
	// show them all unconditionally (a dedicated column with room to spare has
	// no reason to hide anything). When they don't fit, respect the 'g' toggle
	// so users can collapse them to save space.
	totalGlobals := 0
	for _, idx := range m.visible {
		if m.fields[idx].Param.Global {
			totalGlobals++
		}
	}
	availableRows := 20 // reasonable default before the first WindowSizeMsg
	if m.vpReady && m.vp.Height > 0 {
		availableRows = m.vp.Height
	}
	fitAll := totalGlobals <= availableRows
	showAllGlobals := m.showGlobals || fitAll

	// Partition visible fields into (req+opt) and (globals).
	var ro []int
	var hasHiddenGlobal bool
	for _, idx := range m.visible {
		f := &m.fields[idx]
		if f.Param.Global {
			if showAllGlobals || f.Enabled {
				globalsCol = append(globalsCol, idx)
			} else {
				hasHiddenGlobal = true
			}
			continue
		}
		ro = append(ro, idx)
	}

	// Cell-count budget from terminal width.
	cell := m.gridCellWidth()
	maxCols := (m.width + gridCellGap) / (cell + gridCellGap)
	if maxCols > gridMaxCols {
		maxCols = gridMaxCols
	}

	// A globals column is "present" (occupies a slot) whenever the command
	// has any global arguments — visible or hidden-but-hintable.
	globalPresent := len(globalsCol) > 0 || hasHiddenGlobal

	// Req+Opt: 1 col unless count > threshold AND we have room for 2 ro cols
	// plus (if needed) the globals col.
	roColCount := 1
	need := 2
	if globalPresent {
		need = 3
	}
	if len(ro) > gridSplitThreshold && maxCols >= need {
		roColCount = 2
	}

	// Distribute ro across roColCount, required-first order preserved.
	if roColCount == 1 {
		if len(ro) > 0 {
			roCols = [][]int{ro}
		}
	} else {
		half := (len(ro) + 1) / 2 // ceil
		roCols = [][]int{ro[:half], ro[half:]}
	}

	cols = roColCount
	if globalPresent {
		cols++
	} else {
		globalsCol = nil
	}
	return
}

// GridLayout is a test accessor mirroring gridLayout.
func (m Form) GridLayout() (roCols [][]int, globalsCol []int, cols int) {
	return m.gridLayout()
}

// moveCursorVert moves the cursor to the previous (delta = -1) or next
// (delta = +1) row in the visual grid. On top of a non-first column, it
// wraps to the previous column's last row; on the bottom of a non-last
// column, it wraps to the next column's first row. Hard stops at the
// very top of the first column and the very bottom of the last column.
// In single-column mode (cols < 2) it falls back to a flat step through
// m.visible. Returns true if the cursor moved.
func (m *Form) moveCursorVert(delta int) bool {
	roCols, globalsCol, cols := m.gridLayout()
	if cols < 2 {
		next := m.cursor + delta
		if next < 0 || next >= len(m.visible) {
			return false
		}
		m.cursor = next
		return true
	}
	all := append([][]int(nil), roCols...)
	if len(globalsCol) > 0 {
		all = append(all, globalsCol)
	}
	focusIdx := m.fieldAt(m.cursor)
	if focusIdx < 0 {
		return false
	}
	curCol, curRow := -1, -1
	for c, col := range all {
		for r, idx := range col {
			if idx == focusIdx {
				curCol, curRow = c, r
				break
			}
		}
		if curCol >= 0 {
			break
		}
	}
	if curCol < 0 {
		return false
	}
	newCol, newRow := curCol, curRow+delta
	if newRow < 0 {
		newCol--
		if newCol < 0 {
			return false
		}
		newRow = len(all[newCol]) - 1
	} else if newRow >= len(all[curCol]) {
		newCol++
		if newCol >= len(all) {
			return false
		}
		newRow = 0
	}
	targetIdx := all[newCol][newRow]
	for vi, idx := range m.visible {
		if idx == targetIdx {
			m.cursor = vi
			return true
		}
	}
	return false
}

// moveCursorHoriz jumps the cursor to the same row in the neighbouring
// column (delta = -1 for left, +1 for right). Returns true if the cursor
// moved. When the target column is shorter, lands on its last row.
// No-op when grid is disabled (cols < 2) or when at the border.
func (m *Form) moveCursorHoriz(delta int) bool {
	roCols, globalsCol, cols := m.gridLayout()
	if cols < 2 {
		return false
	}
	all := append([][]int(nil), roCols...)
	if len(globalsCol) > 0 {
		all = append(all, globalsCol)
	}
	focusIdx := m.fieldAt(m.cursor)
	if focusIdx < 0 {
		return false
	}
	curCol, curRow := -1, -1
	for c, col := range all {
		for r, idx := range col {
			if idx == focusIdx {
				curCol, curRow = c, r
				break
			}
		}
		if curCol >= 0 {
			break
		}
	}
	if curCol < 0 {
		return false
	}
	newCol := curCol + delta
	if newCol < 0 || newCol >= len(all) {
		return false
	}
	newRow := curRow
	if newRow >= len(all[newCol]) {
		newRow = len(all[newCol]) - 1
	}
	if newRow < 0 {
		return false
	}
	targetIdx := all[newCol][newRow]
	for vi, idx := range m.visible {
		if idx == targetIdx {
			m.cursor = vi
			return true
		}
	}
	return false
}

// Test accessors. These expose read-only views of Form internals so tests
// can assert against unexported state without importing internal packages.
// Callers outside tests should not depend on this surface.

// Cursor returns the current cursor index into m.visible.
func (m Form) Cursor() int { return m.cursor }

// Mode returns the current interaction mode.
func (m Form) Mode() FormMode { return m.mode }

// Fields returns a defensive copy of all fields.
func (m Form) Fields() []Field { return append([]Field(nil), m.fields...) }

// Visible returns the filtered field-index subset.
func (m Form) Visible() []int { return append([]int(nil), m.visible...) }

// DraftRestored reports whether NewForm loaded a persisted draft.
func (m Form) DraftRestored() bool { return m.draftRestored }

// ShowGlobals reports whether the "g" toggle is showing global params.
func (m Form) ShowGlobals() bool { return m.showGlobals }

// Quitting reports whether the form has signalled tea.Quit.
func (m Form) Quitting() bool { return m.quitting }

// Result returns the assembled command written on submit.
func (m Form) Result() string { return m.result }

// TextInputValue returns the current textinput buffer.
func (m Form) TextInputValue() string { return m.textInput.Value() }

// PickerVarNames returns the var picker's full name list (unfiltered).
func (m Form) PickerVarNames() []string { return m.varPop.AllNames() }

// PickerCols returns the total column count in the var picker.
func (m Form) PickerCols() int { return m.varPop.cols }

// PickerCursor returns the (col, row) cursor position in the var picker.
func (m Form) PickerCursor() (int, int) { return m.varPop.colIdx, m.varPop.rowIdx }

// PickerColNames returns the names visible in var picker column i.
func (m Form) PickerColNames(i int) []string {
	return m.varPop.PickerColNames(i)
}

// ErrorMsg returns the persistent error line shown in the footer.
func (m Form) ErrorMsg() string { return m.errorMsg }

// Hint returns the transient hint line shown in the footer.
func (m Form) Hint() string { return m.hintMsg }

// HintActive reports whether a hint-clear tick is pending.
func (m Form) HintActive() bool { return m.hintActive }

// Findings returns a defensive copy of the current validation findings.
func (m Form) Findings() []validate.Finding {
	return append([]validate.Finding(nil), m.findings...)
}

// SetFindings replaces m.findings. Test-only helper used to drive the
// warning-cycle footer without spinning up the full validation engine.
func (m *Form) SetFindings(findings []validate.Finding) {
	m.findings = findings
	m.warningIdx = 0
}

// SessionVars returns the map of variable names seen this session.
func (m Form) SessionVars() map[string]bool { return m.sessionVars }

// Declarations returns a defensive copy of vars the user explicitly declared.
func (m Form) Declarations() []DeclaredVar {
	return append([]DeclaredVar(nil), m.declaredVars...)
}
