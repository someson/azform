// Package widget embeds the shell integration scripts so the binary can
// emit them on demand (`azform shell-init <shell>`).
//
// The scripts live here rather than being copied by install.sh: a piped
// install (`curl … | sh`) has no repo checkout to copy from, and
// embedding also makes widget/binary version skew impossible — the
// widget a user sources is always the one built into the binary that
// reads it.
package widget

import (
	"embed"
	"errors"
	"fmt"
)

//go:embed widget.zsh widget.bash
var scripts embed.FS

// ErrUnsupportedShell is returned by Script for a shell that has no
// widget. sh and dash have no keybinding mechanism, and bash below 4
// lacks READLINE_LINE/READLINE_POINT; install.sh handles both by
// installing the binary alone.
var ErrUnsupportedShell = errors.New("unsupported shell")

// Shells lists the shells Script accepts, sorted, for usage messages.
func Shells() []string {
	return []string{"bash", "zsh"}
}

// Script returns the widget source for shell, byte-identical to the
// widget/<file> the repo ships.
func Script(shell string) ([]byte, error) {
	var name string
	switch shell {
	case "zsh":
		name = "widget.zsh"
	case "bash":
		name = "widget.bash"
	default:
		return nil, fmt.Errorf("%q: %w", shell, ErrUnsupportedShell)
	}
	data, err := scripts.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", name, err)
	}
	return data, nil
}
