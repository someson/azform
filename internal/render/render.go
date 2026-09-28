// Package render assembles the final az command string from form field values.
package render

import (
	"regexp"
	"strings"
)

// Dialect controls shell escaping. POSIX covers bash and zsh; Fish has its
// own single-quote rules. The PowerShell constant is reserved per spec
// 5.1 / 13.2 to avoid future churn.
type Dialect int

const (
	POSIX      Dialect = iota
	PowerShell         // not implemented until M9
	Fish
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

// EscapeFish escapes s for fish. Clean identifiers are returned bare;
// everything else is single-quoted. Inside fish single quotes only the
// backslash and the single quote are special, so both are backslash-escaped;
// the POSIX close-escape-reopen idiom would leave a bare quote behind.
func EscapeFish(s string) string {
	if s == "" {
		return "''"
	}
	if !needsQuoting(s) && !strings.ContainsRune(s, '%') {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}

var bracedVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// FishVarRefs rewrites POSIX ${NAME} references to fish's {$NAME}, which
// expands the same way; fish rejects ${NAME} as a syntax error. Anything
// else in a var-mode value is left as typed.
func FishVarRefs(s string) string {
	return bracedVarRe.ReplaceAllString(s, "{$$$1}")
}

// needsQuoting reports whether s must be quoted to reach az as one
// unchanged argument in bash and zsh. Besides the obvious metacharacters it
// covers: '#' (starts a comment at the beginning of a word), '~' (tilde
// expansion), '{' / '}' (brace expansion: `{a,b}` becomes two arguments),
// '^' (zsh EXTENDED_GLOB negation), a leading '=' (zsh `=cmd` expands to
// the command's path) and control characters, which are invisible in the
// rendered command.
func needsQuoting(s string) bool {
	if s[0] == '=' {
		return true
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n',
			'$', '`', '\\', '"', '\'',
			'*', '?', '[', ']',
			'!', '&', '|', ';', '<', '>',
			'(', ')',
			'#', '~', '{', '}', '^':
			return true
		}
		if r < 0x20 || r == 0x7f {
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
		if f.IsVar && cmd.Dialect == Fish {
			val = FishVarRefs(val)
		}
		if !f.IsVar {
			switch cmd.Dialect {
			case PowerShell:
				val = EscapePOSIX(val) // placeholder; M9 adds real PS escaping
			case Fish:
				val = EscapeFish(val)
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
