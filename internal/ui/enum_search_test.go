package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
)

func keys(m EnumModel, ks ...tea.KeyMsg) (EnumModel, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range ks {
		m, cmd = m.Update(k)
	}
	return m, cmd
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

var (
	keyEnter = tea.KeyMsg{Type: tea.KeyEnter}
	keyEsc   = tea.KeyMsg{Type: tea.KeyEsc}
	keyDown  = tea.KeyMsg{Type: tea.KeyDown}
	keyBack  = tea.KeyMsg{Type: tea.KeyBackspace}
)

func groups() []string {
	return []string{manualEntryChoice, "dev-jobs", "prod-weu", "prod-neu", "qa-rg", "shared-kv", "test-01", "test-02", "zeta"}
}

func TestEnumSearchNarrowsAndKeepsPinnedRow(t *testing.T) {
	m := NewEnum(groups(), "", 40)
	if h := m.Header(); h != "/ filter" {
		t.Errorf("long list header = %q, want the / filter hint", h)
	}
	// j and q are text while searching, not navigation / cancel.
	m, _ = keys(m, runes("/"), runes("j"), runes("o"))
	if got := strings.Join(m.choices, ","); got != manualEntryChoice+",dev-jobs" {
		t.Fatalf("choices for %q = %q", m.query, got)
	}
	if m.choices[m.cursor] != "dev-jobs" {
		t.Errorf("cursor on %q, want the first match", m.choices[m.cursor])
	}
	if h := m.Header(); h != "/ jo▏" {
		t.Errorf("header = %q", h)
	}
	m, _ = keys(m, keyBack, keyBack, runes("PROD"))
	if got := strings.Join(m.choices[1:], ","); got != "prod-weu,prod-neu" {
		t.Errorf("case-insensitive match = %q", got)
	}
	m, cmd := keys(m, keyDown, keyEnter)
	if sel := cmd().(EnumSelectedMsg); sel.Value != "prod-neu" || sel.Query != "PROD" {
		t.Errorf("selected %+v", sel)
	}
}

func TestEnumSearchEscClearsBeforeClosing(t *testing.T) {
	m := NewEnum(groups(), "", 40)
	m, cmd := keys(m, runes("/"), runes("q"), keyEsc)
	if cmd != nil {
		t.Fatalf("first Esc must end the search, not close the popup")
	}
	if m.searching || m.query != "" || len(m.choices) != len(groups()) {
		t.Errorf("search not reset: searching=%v query=%q n=%d", m.searching, m.query, len(m.choices))
	}
	_, cmd = keys(m, keyEsc)
	if _, ok := cmd().(EnumCancelledMsg); !ok {
		t.Errorf("second Esc should cancel")
	}
}

func TestEnumSearchWithoutPinnedRow(t *testing.T) {
	m := NewEnum([]string{"Standard_LRS", "Premium_LRS"}, "", 40)
	if h := m.Header(); h != "" {
		t.Errorf("short list header = %q, want none", h)
	}
	m, cmd := keys(m, runes("/"), runes("nothing"), keyEnter)
	if len(m.choices) != 0 || cmd != nil {
		t.Errorf("empty result: choices=%v cmd=%v (Enter must be a no-op)", m.choices, cmd != nil)
	}
	lines := buildPopupLines(m.choices, m.cursor, m.Header())
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "/ nothing▏") || !strings.Contains(joined, "(no matches)") {
		t.Errorf("empty-result popup:\n%s", joined)
	}
	if h := enumPopupHeight(m.choices, m.Header()); h != len(lines) {
		t.Errorf("height %d != rendered %d lines", h, len(lines))
	}
}

func TestPopupHeaderRowRendered(t *testing.T) {
	lines := buildPopupLines(groups(), 0, "/ filter")
	if len(lines) != maxPopupItems+3 {
		t.Fatalf("got %d lines, want borders + header + %d items", len(lines), maxPopupItems)
	}
	if !strings.Contains(stripANSI(lines[1]), "/ filter") {
		t.Errorf("header row = %q", stripANSI(lines[1]))
	}
	w := len([]rune(stripANSI(lines[0])))
	for i, ln := range lines {
		if got := len([]rune(stripANSI(ln))); got != w {
			t.Errorf("line %d width %d, want %d: %q", i, got, w, stripANSI(ln))
		}
	}
}

// A search that finds nothing is usually a new name: picking the free-text
// row opens the editor with the query already typed.
func TestManualEntrySeededWithQuery(t *testing.T) {
	f := NewForm("vm show", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, _ := f.Update(MetadataLoadedMsg{Params: []metadata.Parameter{
		{Name: "--resource-group", TakesValue: true, ValueKind: metadata.ValueKindString},
	}})
	f = m.(Form)
	rg := f.FieldIndex("--resource-group")
	f.fields[rg].FetchState = FetchLoaded
	f.fields[rg].FetchedChoices = []string{"dev-rg", "prod-rg"}
	m, _ = f.Update(keyEnter)
	f = m.(Form)
	for _, k := range []tea.KeyMsg{runes("/"), runes("new-rg")} {
		m, _ = f.Update(k)
		f = m.(Form)
	}
	m, cmd := f.Update(keyEnter)
	f = m.(Form)
	m, _ = f.Update(cmd())
	f = m.(Form)
	if f.mode != FormModeEdit || f.textInput.Value() != "new-rg" {
		t.Errorf("mode=%v input=%q, want editor seeded with new-rg", f.mode, f.textInput.Value())
	}
}
