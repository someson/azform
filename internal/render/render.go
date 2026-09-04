// Package render assembles the final az command string from form field values.
package render

import "strings"

// Dialect controls shell escaping. Only POSIX is implemented for M2; the
// PowerShell constant is reserved per spec 5.1 / 13.2 to avoid future churn.
type Dialect int

const (
	POSIX      Dialect = iota
	PowerShell         // not implemented until M9
)

// EscapePOSIX escapes s for POSIX shell per spec 5.1.
// Clean identifiers are returned bare; everything else is single-quoted with
// internal single quotes replaced by the sequence '\”.
func EscapePOSIX(s string) string {
	if s == "" {
		return "''"
	}
	if !needsQuoting(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func needsQuoting(s string) bool {
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n',
			'$', '`', '\\', '"', '\'',
			'*', '?', '[', ']',
			'!', '&', '|', ';', '<', '>',
			'(', ')':
			return true
		}
	}
	return false
}

// FieldValue carries one parameter's value for Build.
type FieldValue struct {
	Name     string // "--resource-group"
	Value    string // literal or var ref like "$RG"
	IsVar    bool   // true → Value is a shell variable reference, not escaped
	IsSwitch bool   // true → bare-bool flag (e.g. --debug); emit just Name, no value
	Enabled  bool   // false → param is excluded from the output
}

// Command describes the command to assemble.
type Command struct {
	Path    string // "storage account create"
	Fields  []FieldValue
	Dialect Dialect
	Multi   bool // force multiline with backslash continuation
	Width   int  // > 0 → auto-multiline when assembled line exceeds Width
}

// Build produces the complete az command string from cmd.
func Build(cmd Command) string {
	base := "az " + cmd.Path
	var args []string
	for _, f := range cmd.Fields {
		if !f.Enabled {
			continue
		}
		// Switches (bare bools like --debug, --help) emit just the flag name;
		// their Value is the parser's structural "true" and would otherwise
		// turn `--debug` into `--debug true`.
		if f.IsSwitch {
			args = append(args, f.Name)
			continue
		}
		if f.Value == "" {
			continue
		}
		val := f.Value
		if !f.IsVar {
			switch cmd.Dialect {
			case PowerShell:
				val = EscapePOSIX(val) // placeholder; M9 adds real PS escaping
			default:
				val = EscapePOSIX(val)
			}
		}
		args = append(args, f.Name+" "+val)
	}
	if len(args) == 0 {
		return base
	}
	if cmd.Multi || (cmd.Width > 0 && len(base+" "+strings.Join(args, " ")) > cmd.Width) {
		return base + " \\\n  " + strings.Join(args, " \\\n  ")
	}
	return base + " " + strings.Join(args, " ")
}
