package main

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// pseudoVersionRe matches the timestamp-hash tail of a Go pseudo-version
// ("v0.0.0-20260926165719-25ff1eef784e"), optionally followed by build
// metadata such as "+dirty".
var pseudoVersionRe = regexp.MustCompile(`\d{14}-[0-9a-f]{12}(\+.*)?$`)

// fillBuildInfo backfills version, commit and date from the Go build info
// when the release ldflags did not set them. Goreleaser sets all three;
// `go install ...@vX.Y.Z` records only the module version, and `go build`
// in a git checkout (make build) records only the VCS stamp. Without this
// both of those report a bare "dev" with no way to tell which code it is.
func fillBuildInfo() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	version, commit, date = resolveBuildInfo(version, commit, date, info)
}

func resolveBuildInfo(v, c, d string, info *debug.BuildInfo) (string, string, string) {
	// Only a tagged module version replaces "dev". A local `go build`
	// reports "(devel)" or, since Go 1.24, a pseudo-version like
	// "v0.0.0-20260926165719-25ff1eef784e+dirty"; both keep "dev", which
	// the update check never nags (see update.compareSemver) and which
	// does not invalidate the metadata cache on every commit. The commit
	// below still says which code it is.
	if v == "dev" {
		mv := info.Main.Version
		if mv != "" && mv != "(devel)" && !strings.Contains(mv, "+") && !pseudoVersionRe.MatchString(mv) {
			v = strings.TrimPrefix(mv, "v")
		}
	}
	if c != "" {
		return v, c, d
	}
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			c = s.Value
			if len(c) > 7 {
				c = c[:7]
			}
		case "vcs.time":
			if d == "" {
				d = s.Value
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if c != "" && modified {
		c += "-dirty"
	}
	return v, c, d
}

// versionString is the one-line version shown by --version, -h and the
// help overlay: "azform 0.4.1 (abc1234, 2026-09-01T10:00:00Z)".
func versionString() string {
	s := "azform " + version
	if commit != "" {
		s += " (" + commit
		if date != "" {
			s += ", " + date
		}
		s += ")"
	}
	return s
}
