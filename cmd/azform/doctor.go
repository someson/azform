package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/someson/azform/internal/metadata"
)

const doctorAuthTimeout = 5 * time.Second

// doctorOptions is the immutable input set for the --doctor report. Fields
// are not modified by runDoctor; the function is pure with respect to its
// inputs (only files on disk and shell env are inspected).
type doctorOptions struct {
	version  string
	commit   string
	date     string
	cacheDir string
	stateDir string
}

type doctorLine struct {
	key, value string
}

type doctorSection struct {
	title string
	lines []doctorLine
}

func runDoctor(ctx context.Context, out io.Writer, opts doctorOptions) int {
	sections := []doctorSection{
		reportAzform(opts),
		reportAzureCLI(ctx),
		reportCache(opts.cacheDir),
		reportAuth(ctx),
		reportState(opts.stateDir),
		reportPlatform(),
	}
	for i, s := range sections {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "=== %s ===\n", s.title)
		for _, line := range s.lines {
			fmt.Fprintf(out, "%s: %s\n", line.key, line.value)
		}
	}
	return 0
}

func reportAzform(o doctorOptions) doctorSection {
	s := doctorSection{title: "azform"}
	s.lines = append(s.lines,
		doctorLine{key: "version", value: o.version},
		doctorLine{key: "state_dir", value: o.stateDir},
		doctorLine{key: "cache_dir", value: o.cacheDir},
	)
	if o.commit != "" {
		s.lines = append(s.lines, doctorLine{key: "commit", value: o.commit})
	}
	if o.date != "" {
		s.lines = append(s.lines, doctorLine{key: "date", value: o.date})
	}
	return s
}

func reportAzureCLI(ctx context.Context) doctorSection {
	s := doctorSection{title: "azure-cli"}
	azPath, err := exec.LookPath("az")
	if err != nil {
		s.lines = append(s.lines, doctorLine{key: "status", value: "az not found on PATH"})
		return s
	}
	resolved, _ := filepath.EvalSymlinks(azPath)
	if resolved == "" {
		resolved = azPath
	}
	s.lines = append(s.lines,
		doctorLine{key: "status", value: "found"},
		doctorLine{key: "path", value: resolved},
	)
	if version, ok := probeAzVersion(ctx, resolved); ok {
		s.lines = append(s.lines, doctorLine{key: "version", value: version})
	} else {
		s.lines = append(s.lines, doctorLine{key: "version", value: "unknown"})
	}
	return s
}

func reportCache(dir string) doctorSection {
	s := doctorSection{title: "cache"}
	s.lines = append(s.lines, doctorLine{key: "dir", value: dir})
	stats := metadata.CacheStats(dir)
	s.lines = append(s.lines,
		doctorLine{key: "files", value: strconv.Itoa(stats.Files)},
		doctorLine{key: "size", value: strconv.FormatInt(stats.Bytes, 10) + " bytes"},
	)
	if !stats.OldestEntry.IsZero() {
		s.lines = append(s.lines,
			doctorLine{key: "oldest", value: stats.OldestEntry.Format(time.RFC3339)},
			doctorLine{key: "newest", value: stats.NewestEntry.Format(time.RFC3339)},
		)
	}
	return s
}

func reportAuth(parent context.Context) doctorSection {
	s := doctorSection{title: "auth"}
	ctx, cancel := context.WithTimeout(parent, doctorAuthTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "az", "account", "get-access-token",
		"--resource", "https://management.azure.com", "--output", "json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			s.lines = append(s.lines, doctorLine{key: "status", value: "timeout — az did not respond within 5s"})
			return s
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = oneLine(string(out))
		}
		if msg == "" {
			msg = err.Error()
		}
		if isAzMissingMessage(msg) {
			s.lines = append(s.lines, doctorLine{key: "status", value: "az unavailable — cannot check auth"})
			return s
		}
		s.lines = append(s.lines,
			doctorLine{key: "status", value: "not signed in"},
			doctorLine{key: "error", value: oneLine(msg)},
		)
		return s
	}
	var resp struct {
		SubscriptionID string `json:"subscription"`
		TenantID       string `json:"tenant"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || resp.SubscriptionID == "" {
		s.lines = append(s.lines, doctorLine{key: "status", value: "signed in (response unparseable)"})
		return s
	}
	s.lines = append(s.lines,
		doctorLine{key: "status", value: "signed_in"},
		doctorLine{key: "subscription", value: resp.SubscriptionID},
		doctorLine{key: "tenant", value: resp.TenantID},
	)
	return s
}

func reportState(stateDir string) doctorSection {
	s := doctorSection{title: "state"}
	s.lines = append(s.lines, doctorLine{key: "dir", value: stateDir})
	var keys []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "AZFORM_") {
			keys = append(keys, kv)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		s.lines = append(s.lines, doctorLine{key: "overrides", value: "(none)"})
		return s
	}
	for _, kv := range keys {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		s.lines = append(s.lines, doctorLine{key: kv[:eq], value: kv[eq+1:]})
	}
	return s
}

func reportPlatform() doctorSection {
	s := doctorSection{title: "platform"}
	s.lines = append(s.lines,
		doctorLine{key: "os", value: runtime.GOOS},
		doctorLine{key: "arch", value: runtime.GOARCH},
		doctorLine{key: "go", value: runtime.Version()},
	)
	if shell := os.Getenv("SHELL"); shell != "" {
		s.lines = append(s.lines, doctorLine{key: "shell", value: shell})
	}
	return s
}

func probeAzVersion(parent context.Context, azPath string) (string, bool) {
	ctx, cancel := context.WithTimeout(parent, doctorAuthTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, azPath, "version", "--output", "json")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	var payload struct {
		AzureCLI string `json:"azure-cli"`
	}
	if err := json.Unmarshal(out, &payload); err != nil || payload.AzureCLI == "" {
		return "", false
	}
	return payload.AzureCLI, true
}

func isAzMissingMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "executable file not found") ||
		strings.Contains(lower, "no such file") ||
		strings.Contains(lower, "not found") && strings.Contains(lower, "az")
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:197] + "..."
	}
	return s
}
