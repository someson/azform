package shell_test

import (
	"testing"

	"github.com/someson/azform/internal/shell"
)

func TestParseRawSimple(t *testing.T) {
	raw, ok := shell.ParseRaw("az group create", 0)
	if !ok {
		t.Fatal("ParseRaw returned false, want true")
	}
	if raw.CommandPath != "group create" {
		t.Errorf("CommandPath = %q, want \"group create\"", raw.CommandPath)
	}
	if raw.Prefix != "" {
		t.Errorf("Prefix = %q, want \"\"", raw.Prefix)
	}
	if raw.Suffix != "" {
		t.Errorf("Suffix = %q, want \"\"", raw.Suffix)
	}
	if raw.Inline {
		t.Error("Inline should be false for top-level az")
	}
}

func TestParseRawWithFlags(t *testing.T) {
	raw, ok := shell.ParseRaw("az group create --name my-group --location westeurope", 0)
	if !ok {
		t.Fatal("ParseRaw returned false")
	}
	if raw.CommandPath != "group create" {
		t.Errorf("CommandPath = %q, want \"group create\"", raw.CommandPath)
	}
	if len(raw.FlagTokens) != 4 {
		t.Errorf("FlagTokens count = %d, want 4 (--name, my-group, --location, westeurope)", len(raw.FlagTokens))
	}
}

func TestParseRawNoAz(t *testing.T) {
	_, ok := shell.ParseRaw("echo hello world", 0)
	if ok {
		t.Error("ParseRaw should return false when no az in buffer")
	}
}

func TestParseRawEmpty(t *testing.T) {
	_, ok := shell.ParseRaw("", 0)
	if ok {
		t.Error("ParseRaw should return false for empty line")
	}
}

func TestParseRawCmdSubst(t *testing.T) {
	line := "RG=$(az group list --query \"[0].name\" -o tsv)"
	raw, ok := shell.ParseRaw(line, 5) // cursor inside $(az...)
	if !ok {
		t.Fatal("ParseRaw returned false")
	}
	if raw.CommandPath != "group list" {
		t.Errorf("CommandPath = %q, want \"group list\"", raw.CommandPath)
	}
	if raw.Prefix != "RG=$(" {
		t.Errorf("Prefix = %q, want \"RG=$(\"", raw.Prefix)
	}
	if raw.Suffix != ")" {
		t.Errorf("Suffix = %q, want \")\"", raw.Suffix)
	}
	if !raw.Inline {
		t.Error("Inline should be true for nested az")
	}
}

func TestParseRawBacktick(t *testing.T) {
	line := "echo `az account show --query id -o tsv`"
	raw, ok := shell.ParseRaw(line, 10) // cursor inside backtick
	if !ok {
		t.Fatal("ParseRaw returned false")
	}
	if raw.CommandPath != "account show" {
		t.Errorf("CommandPath = %q, want \"account show\"", raw.CommandPath)
	}
	if raw.Prefix != "echo `" {
		t.Errorf("Prefix = %q, want \"echo `\"", raw.Prefix)
	}
	if raw.Suffix != "`" {
		t.Errorf("Suffix = %q, want \"`\"", raw.Suffix)
	}
	if !raw.Inline {
		t.Error("Inline should be true for backtick-nested az")
	}
}

func TestParseRawSuffixPreserved(t *testing.T) {
	line := "az group create --name x && echo done"
	raw, ok := shell.ParseRaw(line, 5) // cursor in first segment
	if !ok {
		t.Fatal("ParseRaw returned false")
	}
	if raw.CommandPath != "group create" {
		t.Errorf("CommandPath = %q, want \"group create\"", raw.CommandPath)
	}
	if raw.Suffix != " && echo done" {
		t.Errorf("Suffix = %q, want \" && echo done\"", raw.Suffix)
	}
}

func TestParseRawCursorSelectsTarget(t *testing.T) {
	// Two top-level az commands; cursor in second
	line := "az group list; az vm list"
	raw, ok := shell.ParseRaw(line, 16) // cursor inside "az vm list"
	if !ok {
		t.Fatal("ParseRaw returned false")
	}
	if raw.CommandPath != "vm list" {
		t.Errorf("CommandPath = %q, want \"vm list\"", raw.CommandPath)
	}
}

func TestParseRawLineContinuation(t *testing.T) {
	line := "az group create \\\n  --name my-group"
	raw, ok := shell.ParseRaw(line, 0)
	if !ok {
		t.Fatal("ParseRaw returned false")
	}
	if raw.CommandPath != "group create" {
		t.Errorf("CommandPath = %q, want \"group create\"", raw.CommandPath)
	}
	if len(raw.FlagTokens) != 2 { // --name, my-group
		t.Errorf("FlagTokens = %d, want 2", len(raw.FlagTokens))
	}
}
