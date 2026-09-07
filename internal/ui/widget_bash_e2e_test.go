package ui_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// TestE2EBashWidgetEnvOut drives a real interactive bash through a pty,
// fires the widget with Ctrl+X A, queues a variable via the g-popup and
// cancels out, then asserts the env-out file the widget evals holds the
// committed line.
//
// Unlike the zsh e2e tests in widget_e2e_test.go — which spawn azform
// directly and never exercise a shell widget — this one has to go
// through bash, because READLINE_LINE/READLINE_POINT only exist inside
// a `bind -x` callback. That round trip is the thing under test.
//
// Skips when no bash >= 4 is available so a runner with only bash 3.2
// (macos-latest) stays green instead of failing.
func TestE2EBashWidgetEnvOut(t *testing.T) {
	bash := bashAtLeast4(t)
	if bash == "" {
		skipOrFail(t, "no bash >= 4 available")
	}
	bin := repoBinary(t)
	if bin == "" {
		skipOrFail(t, "bin/azform not built; run `make build` first")
	}

	tmp := t.TempDir()
	keep := tmp + "/env-out"
	rc := tmp + "/rc"
	widgetPath, err := filepath.Abs(repoRoot(t) + "/widget/widget.bash")
	if err != nil {
		t.Fatalf("resolve widget path: %v", err)
	}
	rcBody := "source " + widgetPath + "\nPS1='PROMPT> '\n"
	if err := os.WriteFile(rc, []byte(rcBody), 0o600); err != nil {
		t.Fatalf("write rc: %v", err)
	}

	// Put the freshly built binary first on PATH, resolved absolutely.
	// $PWD is the *inherited* shell working directory, not the test's,
	// so building a path from it is wrong — and locally it was masked
	// by ~/.local/bin/azform from `make install`, meaning this test was
	// silently exercising the installed binary rather than bin/azform.
	binDir, err := filepath.Abs(filepath.Dir(bin))
	if err != nil {
		t.Fatalf("resolve binary dir: %v", err)
	}

	cmd := exec.Command(bash, "--noprofile", "--rcfile", rc, "-i")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TERM=xterm-256color",
		"AZFORM_NO_UPDATE_CHECK=1",
		"AZFORM_ENV_OUT_KEEP="+keep,
	)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Capture the pty stream instead of discarding it. The widget runs
	// inside bash, so anything it or its helpers write to the terminal
	// — command-not-found, mktemp errors, azform diagnostics — only
	// surfaces here. Discarding it made a CI-only failure impossible to
	// diagnose from the logs.
	var mu sync.Mutex
	var ptyOut strings.Builder
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				mu.Lock()
				ptyOut.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	dumpPty := func() string {
		mu.Lock()
		defer mu.Unlock()
		return ptyOut.String()
	}

	seq := func(s string, perByte, settle time.Duration) {
		for _, b := range []byte(s) {
			_, _ = io.WriteString(f, string(b))
			time.Sleep(perByte)
		}
		time.Sleep(settle)
	}

	time.Sleep(1200 * time.Millisecond)
	seq("az group create", 20*time.Millisecond, 400*time.Millisecond)
	seq("\x18a", 60*time.Millisecond, 7*time.Second) // Ctrl+X A -> TUI
	seq("g", 60*time.Millisecond, 500*time.Millisecond)
	seq("bashVar=value1", 40*time.Millisecond, 500*time.Millisecond)
	seq("\r", 80*time.Millisecond, 500*time.Millisecond) // commit the line
	seq("\r", 80*time.Millisecond, 500*time.Millisecond) // close the popup
	seq("\x1b", 100*time.Millisecond, 3*time.Second)     // Esc: cancel still flushes

	_ = cmd.Process.Kill()
	waited := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
	}

	data, err := os.ReadFile(keep)
	if err != nil {
		t.Fatalf("read env-out: %v\n--- pty output ---\n%s\n--- end ---", err, dumpPty())
	}
	if !strings.Contains(string(data), "bashVar='value1'") {
		t.Fatalf("env-out missing queued var; got:\n%s\n--- pty output ---\n%s\n--- end ---", data, dumpPty())
	}
}
