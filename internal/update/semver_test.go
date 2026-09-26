package update

import "testing"

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.2.0", -1},
		{"0.10.0", "0.9.0", 1},
		{"v1.0.0", "1.0.0", 0},
		{"1.2", "1.2.0", 0},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0+build.5", "1.0.0", 0},
		{"0.3.0-5-gabcdef", "0.3.0", -1},
		{"0.3.0-5-gabcdef", "0.3.1", -1},
	}
	for _, tc := range cases {
		if got := compareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	// A local "dev" build never reports an update.
	if got := newerThan("dev", "9.9.9"); got != "" {
		t.Errorf("newerThan(dev) = %q, want empty", got)
	}
	if got := newerThan("1.0.0-rc1", "1.0.0"); got != "1.0.0" {
		t.Errorf("newerThan(rc1, 1.0.0) = %q, want 1.0.0", got)
	}
}
