// dev-render exercises a Form and prints its View() — useful for
// eyeballing layout changes without spinning up the real shell widget.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/ui"
)

func main() {
	mode := "single"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	params := gridParams()
	if mode == "single" {
		params = singleParams()
	}
	f := ui.NewForm("network public-ip create", "/tmp/out.txt", "/tmp/state", "test", nil)
	m, _ := f.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m, _ = m.(ui.Form).Update(ui.MetadataLoadedMsg{Params: params, Summary: "Create a public IP address."})
	if len(os.Args) > 2 && os.Args[2] == "enum" {
		// Open the enum popup over the first optional field (--allocation-method
		// in grid mode, --sku in single mode). Required fields come first
		// in the alphabetical-within-group sort, so we skip past them.
		form := m.(ui.Form)
		fields := form.Fields()
		target := -1
		for i, f := range fields {
			if !f.Param.Required && f.Param.HasSelectChoices() {
				target = i
				break
			}
		}
		if target > 0 {
			for i := 0; i < target; i++ {
				m, _ = m.(ui.Form).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
			}
			m, _ = m.(ui.Form).Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	fmt.Println(stripANSI(m.View()))
}

func gridParams() []metadata.Parameter {
	return []metadata.Parameter{
		{Name: "--name", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--resource-group", Aliases: []string{"-g"}, Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--location", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--allocation-method", TakesValue: true, ValueKind: metadata.ValueKindEnum, Choices: []string{"Dynamic", "Static"}, Group: "Optional Parameters"},
		{Name: "--idle-timeout", TakesValue: true, ValueKind: metadata.ValueKindInt, Group: "Optional Parameters"},
		{Name: "--sku", TakesValue: true, ValueKind: metadata.ValueKindEnum, Choices: []string{"Basic", "Standard"}, Group: "Optional Parameters"},
		{Name: "--version", TakesValue: true, ValueKind: metadata.ValueKindEnum, Choices: []string{"IPv4", "IPv6"}, Group: "Optional Parameters"},
		{Name: "--tags", TakesValue: true, ValueKind: metadata.ValueKindKeyValue, Group: "Optional Parameters"},
		{Name: "--tier", TakesValue: true, ValueKind: metadata.ValueKindEnum, Choices: []string{"Global", "Regional"}, Group: "Optional Parameters"},
		{Name: "--zone", TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Optional Parameters"},
		{Name: "--public-ip-prefix", TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Optional Parameters"},
		{Name: "--reverse-fqdn", TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Optional Parameters"},
		{Name: "--dns-name", TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Optional Parameters"},
	}
}

func singleParams() []metadata.Parameter {
	return []metadata.Parameter{
		{Name: "--name", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--resource-group", Aliases: []string{"-g"}, Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--location", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Required Parameters"},
		{Name: "--sku", TakesValue: true, ValueKind: metadata.ValueKindEnum, Choices: []string{"Standard_LRS", "Premium_LRS"}, Group: "Optional Parameters"},
		{Name: "--tags", TakesValue: true, ValueKind: metadata.ValueKindKeyValue, Group: "Optional Parameters"},
		{Name: "--kind", TakesValue: true, ValueKind: metadata.ValueKindString, Group: "Optional Parameters"},
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
