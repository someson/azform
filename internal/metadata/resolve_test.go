package metadata

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerAddsOnlyShowErrors(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "az")
	contents := `#!/bin/sh
printf '%s\n' "$*" >> "$AZFORM_CAPTURE"
if [ "$1" = "version" ]; then
  printf '%s\n' '{"azure-cli":"2.89.1"}'
  exit 0
fi
cat <<'HELP'

Command
    az group create : Create a new resource group.

Arguments
    --name -n [Required] : Name of the new resource group.
HELP
`
	if err := os.WriteFile(script, []byte(contents), 0o755); err != nil {
		t.Fatalf("write fake az: %v", err)
	}
	t.Setenv("AZFORM_CAPTURE", capture)

	runner := &ExecRunner{AZPath: script, Timeout: 2 * time.Second}
	doc, version, err := runner.Resolve(context.Background(), []string{"group", "create"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if version != "2.89.1" || doc.Command.Command != "group create" {
		t.Fatalf("unexpected result: version=%q doc=%+v", version, doc)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	calls := string(data)
	if !strings.Contains(calls, "group create --help --only-show-errors") {
		t.Fatalf("help call missing --only-show-errors: %q", calls)
	}
	if !strings.Contains(calls, "version --output json") {
		t.Fatalf("version call missing JSON output: %q", calls)
	}
}

func TestExecRunnerRejectsFlagAsCommandSegment(t *testing.T) {
	runner := &ExecRunner{AZPath: "/does/not/matter", Timeout: time.Second}
	if _, _, err := runner.Resolve(context.Background(), []string{"--help"}); err == nil {
		t.Fatalf("flag-like command segment was accepted")
	}
}
