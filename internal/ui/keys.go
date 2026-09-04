package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all key bindings for the form (spec 6.6).
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Toggle     key.Binding // Space: enable/disable param
	Edit       key.Binding // Enter in list: open value editor
	Filter     key.Binding // /: open fuzzy filter
	Tab        key.Binding
	ShiftTab   key.Binding
	Confirm    key.Binding // Enter on Done button
	Quit       key.Binding // Esc/q in list: exit without result
	ShowAll    key.Binding // a: expand collapsed params
	ShowGlobal key.Binding // g: show global args
}

// DefaultKeys matches the keyboard layout in spec 6.6.
var DefaultKeys = KeyMap{
	Up:         key.NewBinding(key.WithKeys("up", "k")),
	Down:       key.NewBinding(key.WithKeys("down", "j")),
	Toggle:     key.NewBinding(key.WithKeys(" ")),
	Edit:       key.NewBinding(key.WithKeys("enter")),
	Filter:     key.NewBinding(key.WithKeys("/")),
	Tab:        key.NewBinding(key.WithKeys("tab")),
	ShiftTab:   key.NewBinding(key.WithKeys("shift+tab")),
	Confirm:    key.NewBinding(key.WithKeys("enter")),
	Quit:       key.NewBinding(key.WithKeys("esc", "q")),
	ShowAll:    key.NewBinding(key.WithKeys("a")),
	ShowGlobal: key.NewBinding(key.WithKeys("g")),
}
