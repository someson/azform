package vars_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/someson/azform/internal/vars"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "vars.dat")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestReadFileBasic(t *testing.T) {
	path := writeFile(t, "RG=my-group\x00LOC=westeurope\x00SA=mystorage\x00")
	got, err := vars.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d vars, want 3: %+v", len(got), got)
	}
	if got[0].Name != "RG" || got[0].Value != "my-group" {
		t.Errorf("got[0] = %+v, want {RG my-group}", got[0])
	}
	if got[2].Name != "SA" || got[2].Value != "mystorage" {
		t.Errorf("got[2] = %+v, want {SA mystorage}", got[2])
	}
}

func TestReadFileValueWithNewline(t *testing.T) {
	path := writeFile(t, "MSG=line1\nline2\x00OK=done\x00")
	got, err := vars.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d vars, want 2", len(got))
	}
	if got[0].Value != "line1\nline2" {
		t.Errorf("got[0].Value = %q, want %q", got[0].Value, "line1\nline2")
	}
}

func TestReadFileFiltersSensitive(t *testing.T) {
	path := writeFile(t, "RG=ok\x00AZURE_CLIENT_SECRET=shh\x00GITHUB_TOKEN=abc\x00DB_PASSWORD=hush\x00API_KEY=xyz\x00SAFE=value\x00")
	got, err := vars.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d vars, want 2 (RG, SAFE): %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, v := range got {
		names[v.Name] = true
	}
	for _, banned := range []string{"AZURE_CLIENT_SECRET", "GITHUB_TOKEN", "DB_PASSWORD", "API_KEY"} {
		if names[banned] {
			t.Errorf("sensitive var %q should be filtered", banned)
		}
	}
}

func TestReadFileSkipsMalformed(t *testing.T) {
	// Records without '=' are skipped silently.
	path := writeFile(t, "GOOD=value\x00NOEQUALS\x00ALSO=ok\x00")
	got, err := vars.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d vars, want 2 (malformed skipped): %+v", len(got), got)
	}
}

func TestReadFileMissingFile(t *testing.T) {
	_, err := vars.ReadFile("/nonexistent/path/vars.dat")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadFileEmpty(t *testing.T) {
	path := writeFile(t, "")
	got, err := vars.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d vars, want 0", len(got))
	}
}

func TestIsSensitiveName(t *testing.T) {
	cases := map[string]bool{
		"RG":                false,
		"LOCATION":          false,
		"PASSWORD":          true,
		"db_passwd":         true,
		"GitHub_Token":      true,
		"AZURE_CLIENT_KEY":  true,
		"MY_CREDENTIAL":     true,
		"connection-string": true,
		"MY_NAME":           false,
	}
	for name, want := range cases {
		if got := vars.IsSensitiveName(name); got != want {
			t.Errorf("IsSensitiveName(%q) = %v, want %v", name, got, want)
		}
	}
}
