package ui_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runInstallLib sources install.sh in library mode (AZFORM_INSTALL_LIB=1
// makes it define its functions and return before the main flow) and
// runs one expression against it, returning trimmed stdout.
func runInstallLib(t *testing.T, expr string) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", "AZFORM_INSTALL_LIB=1 . ./install.sh; "+expr)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run %q: %v", expr, err)
	}
	return strings.TrimSpace(string(out))
}

// TestInstallShWidgetSelection pins which widget each shell gets, and
// that unsupported shells get none. The bash 3 case is the bug this
// work fixes: it used to receive the zsh widget, which made every new
// bash shell print a syntax error at startup.
func TestInstallShWidgetSelection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		shell string
		major string
		want  string
	}{
		{"zsh", "zsh", "0", "widget.zsh"},
		{"bash 5", "bash", "5", "widget.bash"},
		{"bash 4", "bash", "4", "widget.bash"},
		{"bash 3", "bash", "3", ""},
		{"sh", "sh", "0", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runInstallLib(t, "azform_widget_for_shell "+tc.shell+" "+tc.major)
			if got != tc.want {
				t.Errorf("shell=%s major=%s: got %q, want %q", tc.shell, tc.major, got, tc.want)
			}
		})
	}
}

// TestInstallShUnsupportedMessage pins the exact user-facing copy. It
// is the only thing an sh/dash user ever gets from azform, so the
// wording is part of the contract, not incidental.
func TestInstallShUnsupportedMessage(t *testing.T) {
	t.Parallel()
	got := runInstallLib(t, "azform_unsupported_message sh 0")
	want := "widget not installed: your shell is sh; azform's widget supports zsh and bash 4+. Re-run from your target shell."
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestInstallShOldBashMessage covers the other unsupported branch: the
// message must name the version found and must not hardcode a package
// manager, since bash 3.x is a version problem, not a macOS problem.
func TestInstallShOldBashMessage(t *testing.T) {
	t.Parallel()
	got := runInstallLib(t, "azform_unsupported_message bash 3")
	if !strings.Contains(got, "bash 4+") {
		t.Errorf("message should state the requirement; got %q", got)
	}
	if !strings.Contains(got, "3") {
		t.Errorf("message should name the version found; got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "brew") {
		t.Errorf("message must not hardcode a package manager; got %q", got)
	}
}

// TestInstallShWriteWidgetFromOutsideRepo is the regression test for the
// piped-install blocker: `curl … | sh` sets $0 to "sh", so SCRIPT_DIR
// resolved to the user's cwd and the widget copy failed for everyone
// without a repo checkout. write_widget must now get the scripts from
// the binary it just installed, so it has to work with a cwd that has
// no widget/ directory at all.
//
// Running this from the repo root would pass for the wrong reason —
// hence the explicit cmd.Dir outside it.
func TestInstallShWriteWidgetFromOutsideRepo(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	shareDir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(binDir, "azform"), "./cmd/azform")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build azform: %v\n%s", err, out)
	}

	elsewhere := t.TempDir()
	cmd := exec.Command("sh", "-c", "AZFORM_INSTALL_LIB=1 . "+filepath.Join(root, "install.sh")+"; write_widget")
	cmd.Dir = elsewhere
	cmd.Env = append(os.Environ(),
		"AZFORM_BIN_DIR="+binDir,
		"AZFORM_SHARE_DIR="+shareDir,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write_widget from %s: %v\n%s", elsewhere, err, out)
	}

	for _, name := range []string{"widget.zsh", "widget.bash"} {
		got, err := os.ReadFile(filepath.Join(shareDir, name))
		if err != nil {
			t.Errorf("read installed %s: %v", name, err)
			continue
		}
		want, err := os.ReadFile(filepath.Join(root, "widget", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("installed %s differs from widget/%s", name, name)
		}
	}
}

// TestInstallShArchiveName pins the archive filename against goreleaser's
// name_template. The tag carries a leading "v" ("v0.1.1") but
// {{ .Version }} does not, so the installer must strip it. Getting this
// wrong makes every download 404 — which is what shipped in v0.1.0 and
// v0.1.1, undetected, because nothing exercised the download path.
func TestInstallShArchiveName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		version, platform, want string
	}{
		{"v0.1.1", "darwin_arm64", "azform_0.1.1_darwin_arm64.tar.gz"},
		{"v1.2.3", "linux_amd64", "azform_1.2.3_linux_amd64.tar.gz"},
		{"0.1.1", "linux_arm64", "azform_0.1.1_linux_arm64.tar.gz"},
	}
	for _, tc := range cases {
		t.Run(tc.version+"_"+tc.platform, func(t *testing.T) {
			t.Parallel()
			got := runInstallLib(t, "azform_archive_name "+tc.version+" "+tc.platform)
			if got != tc.want {
				t.Errorf("azform_archive_name %s %s = %q, want %q", tc.version, tc.platform, got, tc.want)
			}
		})
	}
}

// TestInstallShVerifyChecksumKeepsCallerVars guards a POSIX-sh footgun that
// broke the download path: functions have no locals, so verify_checksum's
// `archive="$1"` overwrote install_binary's `archive`, and the following
// `tar -xzf "$work/$archive"` got "$work/$work/azform_….tar.gz". Nothing
// caught it because no test ever ran a real download.
func TestInstallShVerifyChecksumKeepsCallerVars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	payload := filepath.Join(dir, "azform_0.1.1_darwin_arm64.tar.gz")
	if err := os.WriteFile(payload, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	// sha256 of "payload"
	sum := "239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5"
	sums := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(sums, []byte(sum+"  azform_0.1.1_darwin_arm64.tar.gz\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := runInstallLib(t, "archive=SENTINEL; work=WORKDIR; verify_checksum "+payload+" "+sums+"; printf '%s|%s' \"$archive\" \"$work\"")
	if got != "SENTINEL|WORKDIR" {
		t.Errorf("verify_checksum clobbered caller variables: got %q, want %q", got, "SENTINEL|WORKDIR")
	}
}
