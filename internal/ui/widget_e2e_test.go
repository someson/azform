package ui_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// TestE2EWidgetEnvOutRoundTrip spawns the real azform binary as a
// subprocess connected to a pty, drives the g-popup flow via
// zle-safe key sequences, presses Done, and verifies the env-out
// file the widget would eval contains the committed line.
//
// This is the end-to-end test of the widget-to-shell handoff: form
// → pendingExports → main.go flush → env-out file → widget eval
// → shell parameter. The form-level tests above cover form →
// pendingExports → FlushPendingEnvExports; the integration here
// proves main.go writes the file (the only piece the unit tests
// can't see).
//
// Skip if zsh is missing (the widget's eval depends on it) or the
// binary isn't built.
func TestE2EWidgetEnvOutRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not on PATH")
	}
	bin := repoBinary(t)
	if bin == "" {
		t.Skip("bin/azform not built; run `make build` first")
	}

	tmpDir := t.TempDir()
	envPath := tmpDir + "/env-out"
	varsPath := tmpDir + "/vars"
	outPath := tmpDir + "/out"
	if err := os.WriteFile(varsPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin,
		"--line", "az group create",
		"--cursor", "0",
		"--out", outPath,
		"--vars", varsPath,
		"--env-out", envPath,
		"--cwd", tmpDir,
		"--no-update-check",
	)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = ptmx.Close()
	}()

	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 200}); err != nil {
		t.Logf("pty.Setsize: %v", err)
	}

	// Drain pty output. The peek test showed the form re-renders
	// frequently (cursor blinks, textinput updates), so we read
	// continuously rather than waiting for specific strings.
	go func() { _, _ = io.Copy(io.Discard, ptmx) }()

	// Give metadata fetch + initial render time to settle. Cold
	// caches can take a few seconds; the form is stable once
	// LoadStateLoaded and no field fetches are pending.
	time.Sleep(15 * time.Second)

	// Drive the form. Each rune gets its own write so bubbletea's
	// textinput sees them as separate KeyRunes messages (mirrors
	// what a real user typing produces).
	writeRunes := func(seq string, perByte, settle time.Duration) {
		for _, r := range seq {
			if _, err := ptmx.Write([]byte(string(r))); err != nil {
				t.Fatalf("write %q: %v", r, err)
			}
			time.Sleep(perByte)
		}
		time.Sleep(settle)
	}

	// Fill the required --name field. az group create's required
	// fields are --name and --location. The first field in the list
	// is --name; press Enter to enter edit mode, type a value, Enter
	// to commit. Then advance to --location, repeat.
	writeRunes("\r", 60*time.Millisecond, 400*time.Millisecond) // Enter on --name
	writeRunes("rg1", 40*time.Millisecond, 300*time.Millisecond)
	writeRunes("\r", 60*time.Millisecond, 400*time.Millisecond) // commit
	writeRunes("j", 60*time.Millisecond, 300*time.Millisecond)   // move down to --location
	writeRunes("\r", 60*time.Millisecond, 400*time.Millisecond) // Enter on --location
	writeRunes("westeurope", 40*time.Millisecond, 300*time.Millisecond)
	writeRunes("\r", 60*time.Millisecond, 400*time.Millisecond) // commit

	// Open g-popup, type the var, commit, close popup.
	writeRunes("g", 60*time.Millisecond, 400*time.Millisecond)
	writeRunes("newVar=value1", 40*time.Millisecond, 400*time.Millisecond)
	writeRunes("\r", 80*time.Millisecond, 400*time.Millisecond) // commit
	writeRunes("\r", 80*time.Millisecond, 400*time.Millisecond) // close popup

	// Tab to focus Done, Enter to confirm.
	writeRunes("\t", 80*time.Millisecond, 400*time.Millisecond)
	writeRunes("\r", 80*time.Millisecond, 1500*time.Millisecond) // confirm Done

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Logf("azform exited with (possibly expected) error: %v", err)
		}
	case <-time.After(20 * time.Second):
		if data, err := os.ReadFile(envPath); err == nil {
			t.Logf("env-out at hang time (%d bytes):\n%s", len(data), string(data))
		} else {
			t.Logf("could not read env-out at hang time: %v", err)
		}
		_ = cmd.Process.Kill()
		t.Fatal("azform did not exit after Done")
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env-out: %v", err)
	}
	got := strings.TrimRight(string(data), "\n")
	t.Logf("env-out file contents:\n%s", got)
	if !strings.Contains(got, "newVar='value1'") {
		t.Fatalf("env-out missing line; got:\n%s", got)
	}

	// Round-trip through zsh eval: feed the file to a fresh zsh
	// and confirm the var lands in the shell's parameter table.
	var stdout bytes.Buffer
	zsh := exec.Command("zsh", "-c",
		`while IFS= read -r line; do eval "$line"; done < `+envPath+`
print -r -- "newVar=$newVar"`)
	zsh.Stdout = &stdout
	zsh.Stderr = &stdout
	if err := zsh.Run(); err != nil {
		t.Fatalf("zsh eval: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "newVar=value1") {
		t.Fatalf("after eval, $newVar not visible; output:\n%s", stdout.String())
	}
}

// TestE2ECancelFlushesEnvOut covers the Esc/q cancel path: the user
// queues a var via g-popup, then changes their mind and presses q
// to discard the command. The var must still land in the shell —
// Cancel discards the *command*, not the *queued vars*. This is the
// regression test for the user complaint "I pressed Esc/q and the
// var didn't survive azform exit".
func TestE2ECancelFlushesEnvOut(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not on PATH")
	}
	bin := repoBinary(t)
	if bin == "" {
		t.Skip("bin/azform not built; run `make build` first")
	}

	// Use a fixed prefix under the system temp dir so the
	// directory survives test cleanup on failure for inspection.
	tmpDir, err := os.MkdirTemp("", "azform-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("tmpDir kept for inspection: %s", tmpDir)
		} else {
			os.RemoveAll(tmpDir)
		}
	})
	envPath := tmpDir + "/env-out"
	varsPath := tmpDir + "/vars"
	outPath := tmpDir + "/out"
	if err := os.WriteFile(varsPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin,
		"--line", "az group create",
		"--cursor", "0",
		"--out", outPath,
		"--vars", varsPath,
		"--env-out", envPath,
		"--cwd", tmpDir,
		"--no-update-check",
	)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = ptmx.Close()
	}()
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 200}); err != nil {
		t.Logf("pty.Setsize: %v", err)
	}
	go func() {
		buf, _ := os.Create(tmpDir + "/pty.log")
		defer buf.Close()
		_, _ = io.Copy(buf, ptmx)
	}()

	// Wait for the form to settle.
	time.Sleep(15 * time.Second)

	writeRunes := func(seq string, perByte, settle time.Duration) {
		for _, r := range seq {
			if _, err := ptmx.Write([]byte(string(r))); err != nil {
				t.Fatalf("write %q: %v", r, err)
			}
			time.Sleep(perByte)
		}
		time.Sleep(settle)
	}

	// Open popup, queue a var. Skip filling required fields — we want
	// to make sure cancel still flushes env-out even when the user
	// never reached Done.
	writeRunes("g", 100*time.Millisecond, 800*time.Millisecond)
	writeRunes("newVar=value1", 80*time.Millisecond, 800*time.Millisecond)
	writeRunes("\r", 150*time.Millisecond, 800*time.Millisecond) // commit
	writeRunes("\r", 150*time.Millisecond, 800*time.Millisecond) // close popup

	// Esc from list mode closes the form (confirmCancel path).
	writeRunes("\033", 150*time.Millisecond, 3*time.Second)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Logf("azform exited with (possibly expected) error: %v", err)
		}
	case <-time.After(20 * time.Second):
		if data, err := os.ReadFile(envPath); err == nil {
			t.Logf("env-out at hang time (%d bytes):\n%s", len(data), string(data))
		}
		_ = cmd.Process.Kill()
		t.Fatal("azform did not exit after q")
	}

	// The command file should NOT be written on cancel (no commit
	// happened), but env-out must be.
	if _, err := os.Stat(outPath); err == nil {
		t.Errorf("out file should not exist on cancel; stat: %v", err)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env-out: %v", err)
	}
	got := strings.TrimRight(string(data), "\n")
	if !strings.Contains(got, "newVar='value1'") {
		t.Fatalf("cancel-path env-out missing line; got:\n%s", got)
	}

	// And the round-trip still works on cancel-path output.
	var stdout bytes.Buffer
	zsh := exec.Command("zsh", "-c",
		`while IFS= read -r line; do eval "$line"; done < `+envPath+`
print -r -- "newVar=$newVar"`)
	zsh.Stdout = &stdout
	zsh.Stderr = &stdout
	if err := zsh.Run(); err != nil {
		t.Fatalf("zsh eval: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "newVar=value1") {
		t.Fatalf("cancel-path: after eval, $newVar not visible; output:\n%s", stdout.String())
	}
}

// repoBinary looks for bin/azform relative to the current working
// directory (run from the repo root by `go test ./...`). Empty
// string means not built.
func repoBinary(t *testing.T) string {
	t.Helper()
	for _, p := range []string{"bin/azform", "../bin/azform", "../../bin/azform"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}