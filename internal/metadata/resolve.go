package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// HelpTimeout is deliberately short. The TUI must never be held hostage by
	// a hung Python process; a timeout is a visible error, not a retry loop.
	HelpTimeout = 5 * time.Second
)

// Runner obtains help text and the installed Azure CLI version. It is an
// interface so cache tests can use fixtures without spawning Python.
type Runner interface {
	Resolve(ctx context.Context, commandPath []string) (*Document, string, error)
}

// ExecRunner is the production Runner. Azure CLI is invoked only as a
// subprocess; no Python code is imported or executed in-process.
type ExecRunner struct {
	AZPath  string
	Timeout time.Duration
}

// NewExecRunner returns a runner for the az binary on PATH.
func NewExecRunner() *ExecRunner {
	return &ExecRunner{Timeout: HelpTimeout}
}

// Resolve runs `az <path> --help --only-show-errors` and `az version --output
// json` under one hard timeout, then parses the deterministic help document.
func (r *ExecRunner) Resolve(ctx context.Context, commandPath []string) (*Document, string, error) {
	if len(commandPath) == 0 {
		return nil, "", errors.New("metadata: empty Azure CLI command path")
	}
	for _, part := range commandPath {
		if part == "" {
			return nil, "", errors.New("metadata: empty segment in Azure CLI command path")
		}
		if strings.HasPrefix(part, "-") {
			return nil, "", fmt.Errorf("metadata: invalid Azure CLI command segment %q", part)
		}
	}

	azPath := r.AZPath
	if azPath == "" {
		var err error
		azPath, err = exec.LookPath("az")
		if err != nil {
			return nil, "", fmt.Errorf("metadata: az not found on PATH: %w", err)
		}
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = HelpTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type helpResult struct {
		out []byte
		err error
	}
	type versionResult struct {
		version string
		err     error
	}
	helpCh := make(chan helpResult, 1)
	versionCh := make(chan versionResult, 1)

	go func() {
		args := append(append([]string{}, commandPath...), "--help", "--only-show-errors")
		out, err := runCommand(ctx, azPath, args...)
		helpCh <- helpResult{out: out, err: err}
	}()
	go func() {
		out, err := runCommand(ctx, azPath, "version", "--output", "json")
		if err != nil {
			versionCh <- versionResult{err: err}
			return
		}
		version, err := parseAZVersion(out)
		versionCh <- versionResult{version: version, err: err}
	}()

	help := <-helpCh
	version := <-versionCh
	if help.err != nil {
		return nil, "", fmt.Errorf("metadata: az %s --help failed: %w", strings.Join(commandPath, " "), help.err)
	}
	if version.err != nil {
		return nil, "", fmt.Errorf("metadata: az version failed: %w", version.err)
	}
	doc, err := Parse(string(help.out))
	if err != nil {
		return nil, "", fmt.Errorf("metadata: parse az %s --help: %w", strings.Join(commandPath, " "), err)
	}
	if err := ValidateDocument(doc); err != nil {
		return nil, "", err
	}
	return doc, version.version, nil
}

func runCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(cmd.Environ(), "AZURE_CORE_NO_COLOR=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

func parseAZVersion(out []byte) (string, error) {
	var payload struct {
		AzureCLI string `json:"azure-cli"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", fmt.Errorf("metadata: parse az version JSON: %w", err)
	}
	if payload.AzureCLI == "" {
		return "", errors.New("metadata: az version JSON has no azure-cli field")
	}
	return payload.AzureCLI, nil
}
