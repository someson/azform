package vars

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// LoadAzureDefaults reads Azure CLI defaults from $AZURE_CONFIG_DIR/config
// (default ~/.azure/config), [defaults] section, plus AZURE_DEFAULTS_* env
// vars. Env vars win over config-file values; env vars are also picked up
// even when the config file is absent (per spec §4.4).
func LoadAzureDefaults() []Variable {
	out := mergeAzureDefaults(readAzureConfig(), readAzureEnv())
	return out
}

// readAzureConfig parses the [defaults] section of the Azure CLI config.
// Other sections and malformed lines are silently skipped — a partial or
// future-versioned config must not break form loading.
func readAzureConfig() map[string]string {
	path := azureConfigPath()
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	out := make(map[string]string)
	inDefaults := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inDefaults = strings.EqualFold(line, "[defaults]")
			continue
		}
		if !inDefaults {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if val == "" {
			continue
		}
		out[key] = val
	}
	return out
}

// readAzureEnv returns AZURE_DEFAULTS_* env vars with the prefix stripped and
// lower-cased key. Empty values are skipped (treat as unset).
func readAzureEnv() map[string]string {
	out := make(map[string]string)
	for _, e := range os.Environ() {
		eq := strings.IndexByte(e, '=')
		if eq < 0 {
			continue
		}
		name := e[:eq]
		if !strings.HasPrefix(name, "AZURE_DEFAULTS_") {
			continue
		}
		val := e[eq+1:]
		if val == "" {
			continue
		}
		key := strings.ToLower(strings.TrimPrefix(name, "AZURE_DEFAULTS_"))
		out[key] = val
	}
	return out
}

// mergeAzureDefaults combines config + env (env wins), preserving insertion
// order for deterministic output (config first, then env-only keys).
func mergeAzureDefaults(config, env map[string]string) []Variable {
	seen := make(map[string]bool)
	var out []Variable
	for k, v := range config {
		out = append(out, Variable{Name: k, Value: v})
		seen[k] = true
	}
	for k, v := range env {
		if seen[k] {
			for i := range out {
				if out[i].Name == k {
					out[i].Value = v
					break
				}
			}
		} else {
			out = append(out, Variable{Name: k, Value: v})
		}
	}
	return out
}

func azureConfigPath() string {
	dir := os.Getenv("AZURE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".azure")
	}
	return filepath.Join(dir, "config")
}
