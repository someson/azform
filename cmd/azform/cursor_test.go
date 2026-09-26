package main

import (
	"testing"

	"github.com/someson/azform/internal/shell"
)

func TestCursorByte(t *testing.T) {
	line := "echo ééé && az vm list"
	// The shell reports 21 characters; the same position is 24 bytes.
	if got := cursorByte(line, 21, line); got != len(line) {
		t.Errorf("cursorByte = %d, want %d", got, len(line))
	}
	raw, ok := shell.ParseRaw(line, cursorByte(line, 21, line))
	if !ok || raw.CommandPath != "vm list" {
		t.Errorf("ParseRaw with prefix cursor: %q, %v", raw.CommandPath, ok)
	}
	if got := cursorByte(line, 5, ""); got != 5 {
		t.Errorf("no prefix: got %d, want 5", got)
	}
	if got := cursorByte(line, 5, "unrelated"); got != 5 {
		t.Errorf("mismatched prefix: got %d, want 5", got)
	}
}
