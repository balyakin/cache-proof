package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLoadStarterYAMLIsSafeAndValid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "schemas"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schemas", "product.json"), []byte(`{"type":"object"}`), 0o644))
	path := filepath.Join(dir, "cacheproof.yml")
	require.NoError(t, os.WriteFile(path, []byte(StarterYAML), 0o644))

	cfg, err := Load(context.Background(), path, LoadOptions{})
	require.NoError(t, err)
	require.Len(t, cfg.Scenarios, 2)
	for _, scenario := range cfg.Scenarios {
		require.NotEqual(t, "cold_cache", scenario.Type)
	}
	require.False(t, cfg.Safety.AllowKeyDeletes)
}

func TestDurationMarshalAndDefaults(t *testing.T) {
	var duration Duration
	require.NoError(t, duration.UnmarshalYAML(mustYAMLNode(t, `"2s"`)))
	require.Equal(t, 2*time.Second, duration.Std())
	value, err := duration.MarshalYAML()
	require.NoError(t, err)
	require.Equal(t, "2s", value)

	zero := Duration(0)
	value, err = zero.MarshalYAML()
	require.NoError(t, err)
	require.Equal(t, "", value)

	cfg := Config{Version: 1, Probes: []Probe{{Name: "p", HTTP: &HTTPProbe{Method: "GET", URL: "http://127.0.0.1"}, Assert: Assertion{Expect: "pass"}}}}
	applyDefaults(&cfg, nil)
	require.Equal(t, "127.0.0.1:6380", cfg.Proxy.Listen)
	require.Equal(t, 10000, cfg.Safety.MaxKeysDelete)
	require.Equal(t, 1048576, cfg.MaxValueBytes)
}

func TestDurationUnmarshalEmptyAndInvalid(t *testing.T) {
	var duration Duration
	require.NoError(t, duration.UnmarshalYAML(mustYAMLNode(t, `""`)))
	require.Equal(t, time.Duration(0), duration.Std())
	require.NoError(t, duration.UnmarshalYAML(mustYAMLNode(t, `0`)))
	require.Equal(t, time.Duration(0), duration.Std())
	require.Error(t, duration.UnmarshalYAML(mustYAMLNode(t, `"not-a-duration"`)))
	require.Error(t, duration.UnmarshalYAML(mustYAMLNode(t, `[1]`)))
}

func TestLoadErrors(t *testing.T) {
	_, err := Load(context.Background(), filepath.Join(t.TempDir(), "missing.yml"), LoadOptions{})
	require.Error(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Load(ctx, "unused.yml", LoadOptions{})
	require.Error(t, err)

	path := filepath.Join(t.TempDir(), "bad.yml")
	require.NoError(t, os.WriteFile(path, []byte("version: ["), 0o644))
	_, err = Load(context.Background(), path, LoadOptions{})
	require.Error(t, err)
}

func TestValidateCrossFieldErrors(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.json"), []byte(`{"type":"object"}`), 0o644))
	exitCode := 0
	tests := []struct {
		name    string
		mutate  func(*Config)
		allow   bool
		message string
	}{
		{
			name: "duplicate probe",
			mutate: func(cfg *Config) {
				cfg.Probes[1].Name = cfg.Probes[0].Name
			},
			message: "duplicate probe name",
		},
		{
			name: "duplicate scenario",
			mutate: func(cfg *Config) {
				cfg.Scenarios[1].Name = cfg.Scenarios[0].Name
			},
			message: "duplicate scenario name",
		},
		{
			name: "baseline reserved",
			mutate: func(cfg *Config) {
				cfg.Scenarios[0].Name = "baseline"
			},
			message: "reserved",
		},
		{
			name: "shell without flag",
			mutate: func(cfg *Config) {
				cfg.Probes = []Probe{{Name: "cmd", Command: &CommandSpec{Shell: "go test ./..."}, Assert: Assertion{Expect: "pass", ExitCode: &exitCode}}}
			},
			message: "allow-unsafe-commands",
		},
		{
			name: "unsafe cold cache",
			mutate: func(cfg *Config) {
				cfg.Scenarios = []Scenario{{Name: "cold", Type: "cold_cache"}}
			},
			message: "allow_key_deletes",
		},
		{
			name: "unsafe pattern",
			mutate: func(cfg *Config) {
				cfg.Safety.AllowKeyDeletes = true
				cfg.Profiles[0].KeyPatterns = []string{"*"}
				cfg.Scenarios = []Scenario{{Name: "cold", Type: "cold_cache"}}
			},
			message: "unsafe delete pattern",
		},
		{
			name: "missing schema",
			mutate: func(cfg *Config) {
				cfg.Probes[0].Assert.JSONSchema = "missing.json"
			},
			message: "does not exist",
		},
		{
			name: "probe missing block",
			mutate: func(cfg *Config) {
				cfg.Probes = []Probe{{Name: "empty", Assert: Assertion{Expect: "pass"}}}
			},
			message: "exactly one of http or command",
		},
		{
			name: "command both argv shell",
			mutate: func(cfg *Config) {
				cfg.Probes = []Probe{{Name: "cmd", Command: &CommandSpec{Argv: []string{"true"}, Shell: "true"}, Assert: Assertion{Expect: "pass"}}}
			},
			allow:   true,
			message: "exactly one of argv or shell",
		},
		{
			name: "cold cache without disposable",
			mutate: func(cfg *Config) {
				cfg.Safety.AllowKeyDeletes = true
				cfg.Profiles[0].Disposable = false
				cfg.Scenarios = []Scenario{{Name: "cold", Type: "cold_cache"}}
			},
			message: "disposable profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Probes[0].Assert.JSONSchema = "schema.json"
			tt.mutate(&cfg)
			err := Validate(&cfg, LoadOptions{ConfigDir: dir, AllowUnsafeCommands: tt.allow})
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.message)
		})
	}
}

func TestLoadPasswordEnvExists(t *testing.T) {
	t.Setenv("CACHEPROOF_PASSWORD", "secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "cacheproof.yml")
	yaml := strings.ReplaceAll(StarterYAML, `json_schema: "schemas/product.json"`, `json_schema: ""`)
	yaml = strings.ReplaceAll(yaml, `upstream_password_env: ""`, `upstream_password_env: "CACHEPROOF_PASSWORD"`)
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))
	_, err := Load(context.Background(), path, LoadOptions{})
	require.NoError(t, err)
}

func TestLoadKeepsExplicitZeroWarmupRetries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cacheproof.yml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
proxy:
  listen: "127.0.0.1:6380"
  upstream: "127.0.0.1:6379"
warmup_retries: 0
safety:
  max_keys_delete: 10
  min_delete_prefix: 3
profiles:
  - name: cache
    key_patterns: ["product:*"]
    must_expire: true
    disposable: true
max_value_bytes: 1024
max_keys_scanned: 100
probes:
  - name: p
    http:
      method: GET
      url: "http://127.0.0.1:8000"
      headers: {}
      body: ""
    assert:
      expect: pass
      status: 200
`), 0o644))
	cfg, err := Load(context.Background(), path, LoadOptions{})
	require.NoError(t, err)
	require.Equal(t, 0, cfg.WarmupRetries)
	require.Equal(t, Duration(500*time.Millisecond), cfg.WarmupDelay)
}

func TestLoadMissingPasswordEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cacheproof.yml")
	yaml := strings.ReplaceAll(StarterYAML, `json_schema: "schemas/product.json"`, `json_schema: ""`)
	yaml = strings.ReplaceAll(yaml, `upstream_password_env: ""`, `upstream_password_env: "CACHEPROOF_MISSING_PASSWORD"`)
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	_, err := Load(context.Background(), path, LoadOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "CACHEPROOF_MISSING_PASSWORD")
}

func TestValidateFormatsValidatorErrorsIndividually(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Proxy.Listen = "not-a-host-port"
	cfg.Probes = nil

	err := Validate(&cfg, LoadOptions{})
	require.Error(t, err)
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)
	require.GreaterOrEqual(t, len(validationErr.Messages), 2)
	require.NotContains(t, err.Error(), "Key:")
	require.Contains(t, err.Error(), "Proxy.Listen")
	require.Contains(t, err.Error(), "Probes")
}

func TestValidateFormatsAdditionalValidatorTags(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WarmupRetries = 11
	cfg.Probes[0].HTTP.Method = "TRACE"
	cfg.Probes[0].HTTP.URL = "not-a-url"
	cfg.Probes[0].Assert.JSONSchema = ""

	err := Validate(&cfg, LoadOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "WarmupRetries must be at most 10")
	require.Contains(t, err.Error(), "Probes[0].HTTP.Method must be one of")
	require.Contains(t, err.Error(), "Probes[0].HTTP.URL must be a valid URL")
}

func TestConfigValidateDeletePatternBranches(t *testing.T) {
	require.Error(t, validateDeletePattern("", 3))
	require.Error(t, validateDeletePattern("ab", 3))
	require.Error(t, validateDeletePattern("ab?", 3))
	require.NoError(t, validateDeletePattern("abc", 3))
	require.NoError(t, validateDeletePattern("abc?", 3))
}

func TestLoadDoesNotInjectStarterProbes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cacheproof.yml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
proxy:
  listen: "127.0.0.1:6380"
  upstream: "127.0.0.1:6379"
safety:
  max_keys_delete: 10
  min_delete_prefix: 3
max_value_bytes: 1024
max_keys_scanned: 100
`), 0o644))
	_, err := Load(context.Background(), path, LoadOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Probes")
}

func mustYAMLNode(t *testing.T, raw string) *yaml.Node {
	t.Helper()
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(raw), &node))
	return node.Content[0]
}
