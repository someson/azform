package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveBuildInfo(t *testing.T) {
	vcs := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef"},
		{Key: "vcs.time", Value: "2026-09-01T10:00:00Z"},
		{Key: "vcs.modified", Value: "false"},
	}
	cases := []struct {
		name         string
		v, c, d      string
		mainVersion  string
		settings     []debug.BuildSetting
		wantV, wantC string
		wantD        string
	}{
		{
			name: "ldflags win",
			v:    "0.4.1", c: "abc1234", d: "2026-08-01T00:00:00Z",
			mainVersion: "v9.9.9", settings: vcs,
			wantV: "0.4.1", wantC: "abc1234", wantD: "2026-08-01T00:00:00Z",
		},
		{
			name: "go install at a tag",
			v:    "dev", mainVersion: "v0.4.1",
			wantV: "0.4.1",
		},
		{
			name: "local go build keeps dev, gains commit",
			v:    "dev", mainVersion: "(devel)", settings: vcs,
			wantV: "dev", wantC: "0123456", wantD: "2026-09-01T10:00:00Z",
		},
		{
			name: "local go build pseudo-version keeps dev",
			v:    "dev", mainVersion: "v0.0.0-20260926165719-25ff1eef784e+dirty",
			wantV: "dev",
		},
		{
			name: "go install at a commit keeps dev",
			v:    "dev", mainVersion: "v0.4.2-0.20260926165719-25ff1eef784e",
			wantV: "dev",
		},
		{
			name: "dirty tree",
			v:    "dev", mainVersion: "(devel)",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "0123456789abcdef"},
				{Key: "vcs.modified", Value: "true"},
			},
			wantV: "dev", wantC: "0123456-dirty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Main: debug.Module{Version: tc.mainVersion}, Settings: tc.settings}
			v, c, d := resolveBuildInfo(tc.v, tc.c, tc.d, info)
			if v != tc.wantV || c != tc.wantC || d != tc.wantD {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)", v, c, d, tc.wantV, tc.wantC, tc.wantD)
			}
		})
	}
}
