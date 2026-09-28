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

// fishSession is an interactive fish running in a pty with widget.fish
// sourced and bin/azform first on PATH.
type fishSession struct {
	t    *testing.T
	f    *os.File
	cmd  *exec.Cmd
	tmp  string
	mu   sync.Mutex
	out  strings.Builder
	done bool
}

// startFish launches the session. extraInit runs after the widget is
// sourced, e.g. to define helper functions. Skips when fish or the
// binary is missing.
func startFish(t *testing.T, extraInit string) *fishSession {
	t.Helper()
	fish, err := exec.LookPath("fish")
	if err != nil {
		skipOrFail(t, "fish not installed")
	}
	bin := repoBinary(t)
	if bin == "" {
		skipOrFail(t, "bin/azform not built; run `make build` first")
	}
	widgetPath, err := filepath.Abs(repoRoot(t) + "/widget/widget.fish")
	if err != nil {
		t.Fatalf("resolve widget path: %v", err)
	}
	// Resolve the binary dir absolutely: see TestE2EBashWidgetEnvOut for
	// why a $PWD-relative path silently tested the installed azform.
	binDir, err := filepath.Abs(filepath.Dir(bin))
	if err != nil {
		t.Fatalf("resolve binary dir: %v", err)
	}

	s := &fishSession{t: t, tmp: t.TempDir()}
	init := "function fish_prompt; echo -n 'PROMPT> '; end; set fish_greeting; source " + widgetPath + "; " + extraInit
	s.cmd = exec.Command(fish, "--no-config", "--interactive", "--init-command", init)
	s.cmd.Dir = repoRoot(t)
	s.cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TERM=xterm-256color",
		"AZFORM_NO_UPDATE_CHECK=1",
		"AZFORM_ENV_OUT_KEEP="+s.tmp+"/env-out",
		// Keep drafts, universal variables and history of this run out of
		// the shared default dirs, where later tests would pick them up.
		"XDG_STATE_HOME="+s.tmp+"/state",
		"XDG_CONFIG_HOME="+s.tmp+"/config",
		"XDG_DATA_HOME="+s.tmp+"/data",
	)
	s.f, err = pty.StartWithSize(s.cmd, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	t.Cleanup(s.close)

	// Keep the whole pty stream: anything the widget or azform prints
	// only surfaces here, and it is the only diagnostic on a CI failure.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := s.f.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.out.Write(buf[:n])
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	time.Sleep(1200 * time.Millisecond)
	return s
}

// send types s byte by byte, then waits settle.
func (s *fishSession) send(str string, perByte, settle time.Duration) {
	for _, b := range []byte(str) {
		_, _ = io.WriteString(s.f, string(b))
		time.Sleep(perByte)
	}
	time.Sleep(settle)
}

func (s *fishSession) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

// fail reports msg with the captured pty stream.
func (s *fishSession) fail(format string, args ...any) {
	s.t.Helper()
	s.t.Fatalf(format+"\n--- pty output ---\n%s\n--- end ---", append(args, s.output())...)
}

func (s *fishSession) close() {
	if s.done {
		return
	}
	s.done = true
	_ = s.cmd.Process.Kill()
	waited := make(chan struct{})
	go func() { _, _ = s.cmd.Process.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
	}
	_ = s.f.Close()
}

// TestE2EFishWidgetEnvOut drives a real interactive fish through a pty,
// fires the widget with Ctrl+X A, queues a variable whose value holds a
// single quote via the g-popup and cancels out. It then asserts both the
// env-out file (fish syntax and fish quote escaping) and that the
// variable really exists in the shell afterwards — the widget's eval of
// `set -g NAME 'it\'s'` is what a POSIX-quoted line would have broken.
func TestE2EFishWidgetEnvOut(t *testing.T) {
	s := startFish(t, "")
	s.send("az group create", 20*time.Millisecond, 400*time.Millisecond)
	s.send("\x18a", 60*time.Millisecond, 7*time.Second) // Ctrl+X A -> TUI
	s.send("g", 60*time.Millisecond, 500*time.Millisecond)
	s.send("fishVar=it's", 40*time.Millisecond, 500*time.Millisecond)
	s.send("\r", 80*time.Millisecond, 500*time.Millisecond) // commit the line
	s.send("\r", 80*time.Millisecond, 500*time.Millisecond) // close the popup
	s.send("\x1b", 100*time.Millisecond, 3*time.Second)     // Esc: cancel still flushes

	// The widget evals the env-out line after azform exits; ask the shell
	// for the variable to prove the line survived fish's own parser.
	s.send("\x15echo \"[$fishVar]\"\r", 30*time.Millisecond, 1500*time.Millisecond)
	s.close()

	data, err := os.ReadFile(s.tmp + "/env-out")
	if err != nil {
		s.fail("read env-out: %v", err)
	}
	if !strings.Contains(string(data), `set -g fishVar 'it\'s'`) {
		s.fail("env-out missing queued var; got:\n%s", data)
	}
	if !strings.Contains(s.output(), "[it's]") {
		s.fail("variable not set in the fish session")
	}
}

// TestE2EFishWidgetDone covers the main path: the buffer goes into the
// form, Done writes the command back into fish's command line, and fish
// runs it with the arguments the user meant. A stub `az` function prints
// its argv, so the assertion is on what fish actually passed — a (…)
// substitution from the buffer must have run, and a value with a quote
// must arrive intact.
func TestE2EFishWidgetDone(t *testing.T) {
	s := startFish(t, `function az; printf 'ARGV'; printf '<%s>' $argv; echo; end`)
	// --location's value holds a quote: it must come back quoted with
	// fish's escapes. Both required fields are filled from the buffer, so
	// the test needs no navigation inside the form.
	s.send(`az group create --name (echo sub)-rg --location "it's"`, 20*time.Millisecond, 400*time.Millisecond)
	s.send("\x18a", 60*time.Millisecond, 7*time.Second)      // Ctrl+X A -> TUI
	s.send("\t", 80*time.Millisecond, 400*time.Millisecond)  // focus Done
	s.send("\r", 80*time.Millisecond, 2*time.Second)         // Done -> widget rewrites the line
	s.send("\r", 30*time.Millisecond, 1500*time.Millisecond) // run it

	out := s.output()
	for _, want := range []string{"<--name><sub-rg>", "<--location><it's>"} {
		if !strings.Contains(out, want) {
			s.fail("fish did not pass %s to az", want)
		}
	}
}
