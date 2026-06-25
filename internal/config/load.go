package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"cacheproof/internal/appx"

	"gopkg.in/yaml.v3"
)

func Load(ctx context.Context, path string, opts LoadOptions) (*Config, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: context canceled before loading config: %v", appx.ErrConfig, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read config %q: %v", appx.ErrConfig, path, err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("%w: parse yaml %q: %v", appx.ErrConfig, path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("%w: parse yaml %q: %v", appx.ErrConfig, path, err)
	}
	applyDefaults(&cfg, &document)

	if opts.ConfigDir == "" {
		opts.ConfigDir = filepath.Dir(path)
	}
	if err := Validate(&cfg, opts); err != nil {
		return nil, fmt.Errorf("%w: %v", appx.ErrConfig, err)
	}
	return &cfg, nil
}

func applyDefaults(cfg *Config, document *yaml.Node) {
	if cfg.Proxy.Listen == "" && !hasYAMLKey(document, "proxy", "listen") {
		cfg.Proxy.Listen = "127.0.0.1:6380"
	}
	if cfg.Proxy.Upstream == "" && !hasYAMLKey(document, "proxy", "upstream") {
		cfg.Proxy.Upstream = "127.0.0.1:6379"
	}
	if cfg.Seed == 0 && !hasYAMLKey(document, "seed") {
		cfg.Seed = 1
	}
	if cfg.ProbeTimeout == 0 && !hasYAMLKey(document, "probe_timeout") {
		cfg.ProbeTimeout = Duration(30_000_000_000)
	}
	if cfg.WarmupRetries == 0 && !hasYAMLKey(document, "warmup_retries") {
		cfg.WarmupRetries = 1
	}
	if cfg.WarmupDelay == 0 && !hasYAMLKey(document, "warmup_delay") {
		cfg.WarmupDelay = Duration(500_000_000)
	}
	if cfg.Safety.MaxKeysDelete == 0 && !hasYAMLKey(document, "safety", "max_keys_delete") {
		cfg.Safety.MaxKeysDelete = 10000
	}
	if cfg.Safety.MinDeletePrefix == 0 && !hasYAMLKey(document, "safety", "min_delete_prefix") {
		cfg.Safety.MinDeletePrefix = 3
	}
	if cfg.MaxValueBytes == 0 && !hasYAMLKey(document, "max_value_bytes") {
		cfg.MaxValueBytes = 1048576
	}
	if cfg.MaxKeysScanned == 0 && !hasYAMLKey(document, "max_keys_scanned") {
		cfg.MaxKeysScanned = 5000
	}
	if len(cfg.SuspiciousCommands) == 0 && !hasYAMLKey(document, "suspicious_commands") {
		cfg.SuspiciousCommands = DefaultSuspicious()
	}
}

func hasYAMLKey(document *yaml.Node, path ...string) bool {
	if document == nil || len(path) == 0 {
		return false
	}
	node := document
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	for _, key := range path {
		if node.Kind != yaml.MappingNode {
			return false
		}
		found := false
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == key {
				node = node.Content[index+1]
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
