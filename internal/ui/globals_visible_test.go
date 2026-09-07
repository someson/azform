package ui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/ui"
)

// globalsFixture builds a form shaped like a real `az` command: a couple
// of required flags, a batch of optional ones, and the seven Global
// Arguments every az command carries.
func globalsFixture(t *testing.T, w, h int) ui.Form {
	t.Helper()
	ps := []metadata.Parameter{
		{Name: "--name", Required: true, TakesValue: true},
		{Name: "--resource-group", Required: true, TakesValue: true},
	}
	for _, n := range []string{
		"--allocation-method", "--ddos-protection-mode", "--dns-name",
		"--edge-zone", "--idle-timeout", "--ip-address", "--sku",
		"--tags", "--tier", "--zone",
	} {
		ps = append(ps, metadata.Parameter{Name: n, TakesValue: true})
	}
	for _, n := range []string{"--debug", "--help", "--only-show-errors", "--verbose"} {
		ps = append(ps, metadata.Parameter{Name: n, Global: true, Group: "Global Arguments"})
	}
	for _, n := range []string{"--output", "--query", "--subscription"} {
		ps = append(ps, metadata.Parameter{Name: n, Global: true, TakesValue: true, Group: "Global Arguments"})
	}
	f := ui.NewForm("network public-ip create", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, _ := f.Update(ui.MetadataLoadedMsg{Params: ps, Summary: "Create a public IP address."})
	f = m.(ui.Form)
	m, _ = f.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.(ui.Form)
}

// TestGlobalsAlwaysInLayout pins the behaviour that replaced the G toggle:
// every Global Argument is part of the layout at every terminal size, with
// no hidden state and no key needed to reveal it. On a short terminal they
// may sit below the fold — that is ordinary scrolling, not hiding, so the
// assertion is on the layout rather than on the rendered window.
//
// The toggle this replaced was a no-op in every configuration measured: on
// a tall terminal an internal "they fit, show them anyway" rule overrode
// it, and on a short or narrow one the section stayed collapsed and G could
// not reveal it. The single-column path even printed "press G to show N
// global argument(s)", advertising a key that did nothing.
func TestGlobalsAlwaysInLayout(t *testing.T) {
	t.Parallel()
	const wantGlobals = 7
	sizes := []struct{ w, h int }{{140, 40}, {106, 30}, {106, 12}, {106, 5}, {80, 30}, {80, 10}}
	for _, s := range sizes {
		t.Run(fmt.Sprintf("%dx%d", s.w, s.h), func(t *testing.T) {
			t.Parallel()
			f := globalsFixture(t, s.w, s.h)
			_, globalsCol, cols := f.GridLayout()
			if cols >= 2 && len(globalsCol) != wantGlobals {
				t.Errorf("%dx%d: globalsCol has %d entries, want %d — globals must never be withheld from the layout",
					s.w, s.h, len(globalsCol), wantGlobals)
			}
			if v := f.View(); strings.Contains(v, "press G") {
				t.Errorf("%dx%d: view still advertises the removed G toggle", s.w, s.h)
			}
		})
	}
}

// TestGlobalsRenderWhenRoom is the visible counterpart: on a terminal with
// room for them, globals appear without any keypress.
func TestGlobalsRenderWhenRoom(t *testing.T) {
	t.Parallel()
	v := globalsFixture(t, 140, 40).View()
	for _, want := range []string{"--verbose", "--output", "--subscription"} {
		if !strings.Contains(v, want) {
			t.Errorf("global %s not rendered on a roomy terminal; view:\n%s", want, v)
		}
	}
}
