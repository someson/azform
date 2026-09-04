package vars_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/someson/azform/internal/vars"
)

func TestLoadAzureDefaultsFromConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	if err := os.WriteFile(cfg, []byte(`[defaults]
group = my-group
location = westeurope
`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("AZURE_CONFIG_DIR", dir)

	got := vars.LoadAzureDefaults()
	byName := map[string]string{}
	for _, v := range got {
		byName[v.Name] = v.Value
	}
	if byName["group"] != "my-group" {
		t.Errorf("group = %q, want my-group", byName["group"])
	}
	if byName["location"] != "westeurope" {
		t.Errorf("location = %q, want westeurope", byName["location"])
	}
}

func TestLoadAzureDefaultsEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	if err := os.WriteFile(cfg, []byte(`[defaults]
group = from-config
`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("AZURE_CONFIG_DIR", dir)
	t.Setenv("AZURE_DEFAULTS_GROUP", "from-env")
	t.Setenv("AZURE_DEFAULTS_LOCATION", "eastus")

	got := vars.LoadAzureDefaults()
	byName := map[string]string{}
	for _, v := range got {
		byName[v.Name] = v.Value
	}
	if byName["group"] != "from-env" {
		t.Errorf("group = %q, want from-env (env wins)", byName["group"])
	}
	if byName["location"] != "eastus" {
		t.Errorf("location = %q, want eastus", byName["location"])
	}
}

func TestLoadAzureDefaultsNoConfig(t *testing.T) {
	t.Setenv("AZURE_CONFIG_DIR", "/nonexistent/dir")
	t.Setenv("AZURE_DEFAULTS_GROUP", "")
	t.Setenv("AZURE_DEFAULTS_LOCATION", "")

	got := vars.LoadAzureDefaults()
	if len(got) != 0 {
		t.Errorf("got %+v, want empty", got)
	}
}

func TestLoadAzureDefaultsIgnoresOtherSections(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	if err := os.WriteFile(cfg, []byte(`[storage]
account = mystorage

[defaults]
group = rg1
`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("AZURE_CONFIG_DIR", dir)

	got := vars.LoadAzureDefaults()
	for _, v := range got {
		if v.Name == "account" {
			t.Errorf("non-defaults key should be ignored, got %+v", v)
		}
	}
}

func TestLoadAzureDefaultsComments(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	if err := os.WriteFile(cfg, []byte(`# comment
[defaults]
; another comment
location = westeurope
`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("AZURE_CONFIG_DIR", dir)

	got := vars.LoadAzureDefaults()
	if len(got) != 1 || got[0].Name != "location" || got[0].Value != "westeurope" {
		t.Errorf("got %+v, want one [defaults] location", got)
	}
}
