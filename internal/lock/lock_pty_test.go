//go:build darwin || linux

package lock_test

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/someson/azform/internal/lock"
)

// These tests use real ptys and a real /dev/tty open, because that is the
// only configuration where the bug appears. The other tests in this package
// hand Acquire a regular temp file, whose (dev, inode) differ per file —
// which made "different terminals are independent" pass while the shipped
// binary locked every terminal on the machine against every other.

const helperEnv = "AZFORM_LOCK_HELPER"

// TestLockHelperProcess is not a real test: it is the body of the subprocess
// the tests below run inside a pty. It opens /dev/tty exactly as
// internal/term.Open does, tries to take the lock, reports the outcome, and
// (as the leader) holds it until told to quit so the second attempt is
// guaranteed to overlap.
func TestLockHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("not the helper subprocess")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		report("open-tty-failed:" + err.Error())
		return
	}
	defer func() { _ = tty.Close() }()

	lk, err := lock.Acquire(tty)
	switch {
	case err == nil:
		report(mode + ":ACQUIRED")
	case strings.Contains(err.Error(), lock.ErrLocked.Error()):
		report(mode + ":LOCKED")
	default:
		report(mode + ":ERROR:" + err.Error())
	}
	defer func() { _ = lk.Close() }()

	if mode == "leader" {
		// Second azform in the *same* terminal: must be refused.
		sub := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess", "-test.v=false")
		sub.Env = append(os.Environ(), helperEnv+"=same-terminal")
		sub.Stdout, sub.Stderr = os.Stdout, os.Stderr
		_ = sub.Run()
		// Hold the lock until the parent says the other terminal is done.
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
}

func report(msg string) {
	_, _ = os.Stdout.WriteString("HELPER " + msg + "\n")
}

// startLeader launches a helper in its own pty and waits until it has
// reported both its own result and its same-terminal child's.
func startLeader(t *testing.T, runtimeDir string) (*exec.Cmd, *os.File, []string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess", "-test.v=false")
	cmd.Env = append(os.Environ(), helperEnv+"=leader", "XDG_RUNTIME_DIR="+runtimeDir)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}
	t.Cleanup(func() { _ = ptmx.Close() })

	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(ptmx)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); strings.HasPrefix(line, "HELPER ") {
				lines <- strings.TrimPrefix(line, "HELPER ")
			}
		}
		close(lines)
	}()

	var got []string
	for len(got) < 2 {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("helper exited after reporting %v", got)
			}
			got = append(got, line)
		case <-time.After(30 * time.Second):
			t.Fatalf("timed out waiting for helper; got %v", got)
		}
	}
	return cmd, ptmx, got
}

// TestTwoTerminalsAreIndependent is the regression test for the reported
// bug: azform open in one terminal window silently prevented azform from
// starting in any other terminal on the machine.
func TestTwoTerminalsAreIndependent(t *testing.T) {
	runtimeDir := t.TempDir()

	cmdA, ptmxA, gotA := startLeader(t, runtimeDir)
	if gotA[0] != "leader:ACQUIRED" {
		t.Fatalf("first terminal: got %q, want leader:ACQUIRED", gotA[0])
	}

	// Second terminal, while the first still holds its lock.
	cmdB, ptmxB, gotB := startLeader(t, runtimeDir)
	if gotB[0] != "leader:ACQUIRED" {
		t.Errorf("second terminal was blocked by the first: got %q, want leader:ACQUIRED", gotB[0])
	}

	for _, p := range []*os.File{ptmxA, ptmxB} {
		_, _ = p.WriteString("q\n")
	}
	for _, c := range []*exec.Cmd{cmdA, cmdB} {
		_ = c.Wait()
	}
}

// TestSameTerminalIsRefused is the other half of the contract (spec §15.2):
// pressing the hotkey again in a terminal that already has a form open must
// not start a second instance.
func TestSameTerminalIsRefused(t *testing.T) {
	runtimeDir := t.TempDir()
	cmd, ptmx, got := startLeader(t, runtimeDir)
	if got[0] != "leader:ACQUIRED" {
		t.Fatalf("leader: got %q, want leader:ACQUIRED", got[0])
	}
	if got[1] != "same-terminal:LOCKED" {
		t.Errorf("second instance in the same terminal: got %q, want same-terminal:LOCKED", got[1])
	}
	_, _ = ptmx.WriteString("q\n")
	_ = cmd.Wait()
}
