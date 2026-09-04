// Package update performs the self-update check against GitHub Releases
// (spec §14.4). Network errors are silent — the form never sees them.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrSuppressed is returned when the check is disabled via env or flag.
var ErrSuppressed = errors.New("update: suppressed")

// HTTPClient is the subset of net/http.Client the checker uses.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Options configure a single Check call.
type Options struct {
	Repo        string           // "owner/repo"
	Current     string           // "0.1.0" — current azform version
	StateDir    string           // path to write update-check.json
	HTTPClient  HTTPClient       // injectable; nil → http.DefaultClient
	BaseURL     string           // override endpoint; default GitHub Releases API
	Now         func() time.Time // injectable for throttle tests
	MinInterval time.Duration    // default 24h
}

// Check queries the latest release. Returns the latest version string
// when newer than Options.Current, "" otherwise. Network errors are
// swallowed (returns "", nil) so callers never need to handle them.
// Returns ErrSuppressed when AZFORM_NO_UPDATE_CHECK=1 is set.
func Check(ctx context.Context, opts Options) (string, error) {
	if os.Getenv("AZFORM_NO_UPDATE_CHECK") == "1" {
		return "", ErrSuppressed
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.MinInterval == 0 {
		opts.MinInterval = 24 * time.Hour
	}
	if opts.BaseURL == "" {
		opts.BaseURL = "https://api.github.com"
	}

	// Throttle.
	if cached, ok := loadCached(opts.StateDir); ok && cached.Version != "" {
		if opts.Now().Sub(cached.At) < opts.MinInterval {
			return newerThan(opts.Current, cached.Version), nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/repos/%s/releases/latest", opts.BaseURL, opts.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", nil
	}
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return "", nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", nil
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil
	}
	latest := strings.TrimPrefix(payload.TagName, "v")
	_ = saveCached(opts.StateDir, cachedEntry{At: opts.Now(), Version: latest})
	return newerThan(opts.Current, latest), nil
}

func newerThan(current, candidate string) string {
	if compareSemver(current, candidate) < 0 {
		return candidate
	}
	return ""
}

// compareSemver returns -1/0/1 for a vs b. Numeric components compare
// numerically; non-numeric components fall back to lexical comparison.
func compareSemver(a, b string) int {
	pa := splitSemver(a)
	pb := splitSemver(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		if i >= len(pa) {
			return -1
		}
		if i >= len(pb) {
			return 1
		}
		if pa[i] != pb[i] {
			na, errA := strconv.Atoi(pa[i])
			nb, errB := strconv.Atoi(pb[i])
			if errA == nil && errB == nil {
				if na < nb {
					return -1
				}
				return 1
			}
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitSemver(s string) []string {
	s = strings.TrimPrefix(s, "v")
	return strings.Split(s, ".")
}

type cachedEntry struct {
	At      time.Time `json:"at"`
	Version string    `json:"version"`
}

var cacheMu sync.Mutex

func loadCached(dir string) (cachedEntry, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	data, err := os.ReadFile(filepath.Join(dir, "update-check.json"))
	if err != nil {
		return cachedEntry{}, false
	}
	var e cachedEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return cachedEntry{}, false
	}
	return e, true
}

func saveCached(dir string, e cachedEntry) error {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := json.NewEncoder(tmp).Encode(e); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, "update-check.json"))
}
