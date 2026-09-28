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

// TestE2EFishWidgetEnvOut drives a real interactive fish through a pty,
// fires the widget with Ctrl+X A, queues a variable whose value holds a
// single quote via the g-popup and cancels out. It then asserts both the
// env-out file (fish syntax and fish quote escaping) and that the
// variable really exists in the shell afterwards — the widget's eval of
// `set -g NAME 'it\'s'` is what a POSIX-quoted line would have broken.
//
// Skips when fish is not installed.
func TestE2EFishWidgetEnvOut(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		skipOrFail(t, "fish not installed")
	}
	bin := repoBinary(t)
	if bin == "" {
		skipOrFail(t, "bin/azform not built; run `make build` first")
	}

	tmp := t.TempDir()
	keep := tmp + "/env-out"
	widgetPath, err := filepath.Abs(repoRoot(t) + "/widget/widget.fish")
	if err != nil {
		t.Fatalf("resolve widget path: %v", err)
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

	cmd := exec.Command(fish, "--no-config", "--interactive",
		"--init-command", "function fish_prompt; echo -n 'PROMPT> '; end; set fish_greeting; source "+widgetPath)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TERM=xterm-256color",
		"AZFORM_NO_UPDATE_CHECK=1",
		"AZFORM_ENV_OUT_KEEP="+keep,
		// Keep this run's draft (Esc saves one) out of the shared default
		// state dir, where later e2e tests would restore it.
		"XDG_STATE_HOME="+tmp+"/state",
		"XDG_CONFIG_HOME="+tmp+"/config",
		"XDG_DATA_HOME="+tmp+"/data",
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
	seq("fishVar=it's", 40*time.Millisecond, 500*time.Millisecond)
	seq("\r", 80*time.Millisecond, 500*time.Millisecond) // commit the line
	seq("\r", 80*time.Millisecond, 500*time.Millisecond) // close the popup
	seq("\x1b", 100*time.Millisecond, 3*time.Second)     // Esc: cancel still flushes

	// The widget evals the env-out line after azform exits; ask the shell
	// for the variable to prove the line survived fish's own parser.
	seq("\x15echo \"[$fishVar]\"\r", 30*time.Millisecond, 1500*time.Millisecond)

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
	if !strings.Contains(string(data), `set -g fishVar 'it\'s'`) {
		t.Fatalf("env-out missing queued var; got:\n%s\n--- pty output ---\n%s\n--- end ---", data, dumpPty())
	}
	if !strings.Contains(dumpPty(), "[it's]") {
		t.Fatalf("variable not set in the fish session\n--- pty output ---\n%s\n--- end ---", dumpPty())
	}
}
