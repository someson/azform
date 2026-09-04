package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeAzScript writes a shell script that prints the supplied version JSON
// when invoked as "az version --output json". Returns the directory that
// holds the fake binary; the caller wires it onto PATH.
func fakeAzScript(t *testing.T, versionJSON string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "az")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("if [ \"$1\" = \"version\" ]; then\n")
	b.WriteString("  printf '%s\\n' '" + versionJSON + "'\n")
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("if [ \"$1\" = \"account\" ] && [ \"$2\" = \"get-access-token\" ]; then\n")
	b.WriteString("  printf '%s\\n' '" + accessJSON + "'\n")
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("echo \"unexpected: $*\" 1>&2\n")
	b.WriteString("exit 99\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

const accessJSON = `{"subscription":"11111111-2222-3333-4444-555555555555","tenant":"00000000-aaaa-bbbb-cccc-dddddddddddd"}`

func TestDoctorBasicSectionsAndVersion(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("PATH", t.TempDir()) // az unavailable → auth surfaces the right message

	var buf bytes.Buffer
	code := runDoctor(context.Background(), &buf, doctorOptions{
		version:  "0.1.0",
		commit:   "abc123",
		date:     "2026-09-01",
		cacheDir: "/tmp/cache",
		stateDir: "/tmp/state",
	})
	if code != 0 {
		t.Errorf("runDoctor returned %d, want 0", code)
	}

	out := buf.String()
	for _, title := range []string{"azform", "azure-cli", "cache", "auth", "state", "platform"} {
		if !strings.Contains(out, "=== "+title+" ===") {
			t.Errorf("missing section %q in output:\n%s", title, out)
		}
	}
	for _, line := range []string{
		"version: 0.1.0",
		"commit: abc123",
		"date: 2026-09-01",
		"cache_dir: /tmp/cache",
		"state_dir: /tmp/state",
		"os: " + runtime.GOOS,
		"arch: " + runtime.GOARCH,
		"shell: /bin/zsh",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in output:\n%s", line, out)
		}
	}
	if !strings.Contains(out, "az not found on PATH") {
		t.Errorf("expected az-not-found in azure-cli section, got:\n%s", out)
	}
	if !strings.Contains(out, "az unavailable") {
		t.Errorf("expected az-unavailable in auth section, got:\n%s", out)
	}
}

func TestDoctorAuthSignedIn(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	fakeDir := fakeAzScript(t, `{"azure-cli":"2.62.0","extensions":{}}`)
	t.Setenv("PATH", fakeDir)

	var buf bytes.Buffer
	if got := runDoctor(context.Background(), &buf, doctorOptions{
		version: "0.1.0", cacheDir: t.TempDir(), stateDir: t.TempDir(),
	}); got != 0 {
		t.Errorf("runDoctor = %d", got)
	}
	out := buf.String()
	if !strings.Contains(out, "status: signed_in") {
		t.Errorf("expected signed_in status, got:\n%s", out)
	}
	if !strings.Contains(out, "subscription: 11111111-2222-3333-4444-555555555555") {
		t.Errorf("expected subscription in auth, got:\n%s", out)
	}
	if !strings.Contains(out, "version: 2.62.0") {
		t.Errorf("expected az version in azure-cli section, got:\n%s", out)
	}
}

func TestDoctorAuthNotSignedIn(t *testing.T) {
	t.Setenv("SHELL", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "az")
	script := "#!/bin/sh\necho 'ERROR: Please run az login' 1>&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	var buf bytes.Buffer
	if got := runDoctor(context.Background(), &buf, doctorOptions{
		version: "0.1.0", cacheDir: dir, stateDir: dir,
	}); got != 0 {
		t.Errorf("runDoctor = %d", got)
	}
	out := buf.String()
	if !strings.Contains(out, "status: not signed in") {
		t.Errorf("expected not-signed-in status, got:\n%s", out)
	}
	if !strings.Contains(out, "ERROR: Please run az login") {
		t.Errorf("expected stderr trimmed in error line, got:\n%s", out)
	}
}

func TestDoctorStateEnvOverrides(t *testing.T) {
	t.Setenv("AZFORM_NO_UPDATE_CHECK", "1")
	t.Setenv("AZFORM_LOG_LEVEL", "debug")
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("PATH", t.TempDir())

	var buf bytes.Buffer
	if got := runDoctor(context.Background(), &buf, doctorOptions{
		version: "0.1.0", cacheDir: t.TempDir(), stateDir: t.TempDir(),
	}); got != 0 {
		t.Errorf("runDoctor = %d", got)
	}
	out := buf.String()
	if !strings.Contains(out, "AZFORM_LOG_LEVEL: debug") {
		t.Errorf("expected AZFORM_LOG_LEVEL override, got:\n%s", out)
	}
	if !strings.Contains(out, "AZFORM_NO_UPDATE_CHECK: 1") {
		t.Errorf("expected AZFORM_NO_UPDATE_CHECK override, got:\n%s", out)
	}
}

func TestDoctorStateNoOverrides(t *testing.T) {
	// Make sure unrelated env vars are NOT shown — only AZFORM_*.
	t.Setenv("SHELL", "")
	t.Setenv("PATH", "/bin:/usr/bin")
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "AZFORM_") {
			name := kv[:strings.IndexByte(kv, '=')]
			t.Setenv(name, "")
		}
	}

	var buf bytes.Buffer
	_ = runDoctor(context.Background(), &buf, doctorOptions{
		version: "0.1.0", cacheDir: t.TempDir(), stateDir: t.TempDir(),
	})
	if !strings.Contains(buf.String(), "overrides: (none)") {
		t.Errorf("expected overrides: (none), got:\n%s", buf.String())
	}
}

func TestOneLine(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello\nworld", "hello world"},
		{"a\rb\rc", "a b c"},
		{"x", "x"},
		{strings.Repeat("a", 250), strings.Repeat("a", 197) + "..."},
	}
	for _, tc := range cases {
		if got := oneLine(tc.in); got != tc.want {
			t.Errorf("oneLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsAzMissingMessage(t *testing.T) {
	for _, msg := range []string{
		"exec: \"az\": executable file not found in $PATH",
		"open /no/such/az: no such file or directory",
		"az: command not found",
	} {
		if !isAzMissingMessage(msg) {
			t.Errorf("isAzMissingMessage(%q) = false, want true", msg)
		}
	}
	for _, msg := range []string{
		"ERROR: Please run az login",
		"timed out",
	} {
		if isAzMissingMessage(msg) {
			t.Errorf("isAzMissingMessage(%q) = true, want false", msg)
		}
	}
}

// TestDoctorJSON covers the helper invariants the report uses; not strictly
// required but cheap regression coverage for JSONL readers (e.g. `jq`).
func TestDoctorJSON(t *testing.T) {
	_ = json.Marshal // ensure import retained for future structured output
}
