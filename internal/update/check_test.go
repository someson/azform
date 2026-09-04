package update_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/someson/azform/internal/update"
)

func TestCheckSuppressedByEnv(t *testing.T) {
	t.Setenv("AZFORM_NO_UPDATE_CHECK", "1")
	_, err := update.Check(context.Background(), update.Options{
		Repo:       "owner/repo",
		Current:    "0.1.0",
		StateDir:   t.TempDir(),
		HTTPClient: http.DefaultClient,
	})
	if !errors.Is(err, update.ErrSuppressed) {
		t.Errorf("err = %v, want ErrSuppressed", err)
	}
}

func TestCheckThrottlesWithin24h(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	opts := update.Options{
		Repo:       "owner/repo",
		Current:    "0.1.0",
		StateDir:   dir,
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
		Now:        func() time.Time { return time.Unix(1_700_000_000, 0) },
	}
	if _, err := update.Check(context.Background(), opts); err != nil {
		t.Fatalf("first call: %v", err)
	}
	srv.Close() // ensure no traffic would even matter
	latest, err := update.Check(context.Background(), opts)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if latest != "9.9.9" {
		t.Errorf("latest = %q, want 9.9.9 (cached)", latest)
	}
}

func TestCheckReportsNewVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	opts := update.Options{
		Repo:       "owner/repo",
		Current:    "1.0.0",
		StateDir:   dir,
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
		Now:        func() time.Time { return time.Now() },
	}
	latest, err := update.Check(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if latest != "1.2.3" {
		t.Errorf("latest = %q, want 1.2.3", latest)
	}
}

func TestCheckNoUpdateWhenCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	defer srv.Close()
	opts := update.Options{
		Repo:       "owner/repo",
		Current:    "1.0.0",
		StateDir:   t.TempDir(),
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
		Now:        func() time.Time { return time.Now() },
	}
	latest, err := update.Check(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if latest != "" {
		t.Errorf("latest = %q, want empty", latest)
	}
}

func TestCheckNetworkErrorSilent(t *testing.T) {
	dir := t.TempDir()
	opts := update.Options{
		Repo:       "owner/repo",
		Current:    "0.1.0",
		StateDir:   dir,
		HTTPClient: &http.Client{},
		BaseURL:    "http://127.0.0.1:1", // unreachable
		Now:        func() time.Time { return time.Now() },
	}
	_, err := update.Check(context.Background(), opts)
	if err != nil {
		t.Errorf("expected silent failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "update-check.json")); err == nil {
		t.Error("cache file should not be written on network error")
	}
}
