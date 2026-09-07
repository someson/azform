package ui_test

import (
	"os"
	"testing"
)

// e2eRequired reports whether the environment demands that end-to-end
// tests actually execute. CI sets AZFORM_E2E_REQUIRED=1 on the runner
// where every prerequisite is guaranteed present.
//
// Why this exists: the pty e2e tests guard themselves on external
// prerequisites (a built bin/azform, a zsh, a bash >= 4) and skip when
// one is missing. A skipped test reports as success, so for a long
// time CI was green while the widget round trip never ran at all —
// internal/ui completed in ~1.4s instead of ~60s and nobody noticed.
// Under this flag a missing prerequisite is a hard failure, so "green"
// means the tests really ran.
func e2eRequired() bool { return os.Getenv("AZFORM_E2E_REQUIRED") == "1" }

// skipOrFail skips the test with reason, or fails it when
// AZFORM_E2E_REQUIRED=1. Local runs and contributor machines that lack
// zsh or a modern bash keep skipping politely; CI does not.
func skipOrFail(t *testing.T, reason string) {
	t.Helper()
	if e2eRequired() {
		t.Fatalf("AZFORM_E2E_REQUIRED=1 but prerequisite missing: %s", reason)
	}
	t.Skip(reason)
}
