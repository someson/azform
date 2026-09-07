package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/someson/azform/widget"
)

// runShellInit implements `azform shell-init <shell>`: it writes the
// embedded widget for shell to stdout. install.sh uses it instead of
// copying widget/*.
//
// Exit codes: 0 on success, 2 on any misuse (unknown shell, wrong
// number of arguments) so a typo aborts the installer's `set -e` flow
// rather than truncating a widget file to zero bytes.
func runShellInit(args []string, stdout, stderr io.Writer) int {
	usage := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "azform: "+format+"\n", a...)
		fmt.Fprintf(stderr, "Usage: azform shell-init <%s>\n", strings.Join(widget.Shells(), "|"))
		return 2
	}
	if len(args) != 1 {
		return usage("shell-init takes exactly one shell name")
	}
	script, err := widget.Script(args[0])
	if err != nil {
		return usage("%v", err)
	}
	if _, err := stdout.Write(script); err != nil {
		fmt.Fprintf(stderr, "azform: write widget: %v\n", err)
		return 2
	}
	return 0
}
