package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
)

func Validate(cfg *Config, opts LoadOptions) error {
	var messages []string
	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			for _, fieldErr := range validationErrors {
				messages = append(messages, formatValidationError(fieldErr))
			}
		} else {
			messages = append(messages, err.Error())
		}
	}

	if cfg.Version != 1 {
		messages = append(messages, "version must be 1")
	}
	messages = append(messages, uniqueNames("probe", cfg.Probes, func(p Probe) string { return p.Name })...)
	messages = append(messages, uniqueNames("scenario", cfg.Scenarios, func(s Scenario) string { return s.Name })...)

	for _, scenario := range cfg.Scenarios {
		if strings.EqualFold(scenario.Name, "baseline") {
			messages = append(messages, "scenario name baseline is reserved")
		}
		if scenario.Type == "random_miss" && (scenario.Probability < 0 || scenario.Probability > 1) {
			messages = append(messages, fmt.Sprintf("scenario %q probability must be between 0 and 1", scenario.Name))
		}
		if scenario.Type == "cold_cache" {
			if !hasDisposableProfile(cfg.Profiles) {
				messages = append(messages, "cold_cache requires at least one disposable profile")
			}
			if !cfg.Safety.AllowKeyDeletes {
				messages = append(messages, "cold_cache requires safety.allow_key_deletes: true")
			}
			for _, profile := range cfg.Profiles {
				if !profile.Disposable {
					continue
				}
				for _, pattern := range profile.KeyPatterns {
					if err := validateDeletePattern(pattern, cfg.Safety.MinDeletePrefix); err != nil {
						messages = append(messages, err.Error())
					}
				}
			}
		}
	}

	if cfg.Proxy.UpstreamPasswordEnv != "" {
		if _, ok := os.LookupEnv(cfg.Proxy.UpstreamPasswordEnv); !ok {
			messages = append(messages, fmt.Sprintf("upstream_password_env %q is not set", cfg.Proxy.UpstreamPasswordEnv))
		}
	}
	for _, probe := range cfg.Probes {
		messages = append(messages, validateProbe(probe, opts)...)
	}
	if cfg.ResetCommand != nil {
		messages = append(messages, validateCommand("reset_command", *cfg.ResetCommand, opts.AllowUnsafeCommands)...)
	}

	if len(messages) > 0 {
		return &ValidationError{Messages: messages}
	}
	return nil
}

func formatValidationError(err validator.FieldError) string {
	field := strings.TrimPrefix(err.Namespace(), "Config.")
	switch err.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, err.Param())
	case "min":
		return fmt.Sprintf("%s must be at least %s", field, err.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, err.Param())
	case "hostname_port":
		return fmt.Sprintf("%s must be host:port", field)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	default:
		return fmt.Sprintf("%s failed validation %q", field, err.Tag())
	}
}

func uniqueNames[T any](kind string, rows []T, nameFn func(T) string) []string {
	seen := make(map[string]struct{})
	var messages []string
	for _, row := range rows {
		name := nameFn(row)
		if _, ok := seen[name]; ok {
			messages = append(messages, fmt.Sprintf("duplicate %s name %q", kind, name))
			continue
		}
		seen[name] = struct{}{}
	}
	return messages
}

func validateProbe(probe Probe, opts LoadOptions) []string {
	var messages []string
	if (probe.HTTP == nil) == (probe.Command == nil) {
		messages = append(messages, fmt.Sprintf("probe %q must contain exactly one of http or command", probe.Name))
	}
	if probe.Command != nil {
		messages = append(messages, validateCommand("probe "+probe.Name, *probe.Command, opts.AllowUnsafeCommands)...)
	}
	seenFields := make(map[string]struct{})
	for _, field := range probe.Assert.JSONEqualsBaseline {
		if _, ok := seenFields[field]; ok {
			messages = append(messages, fmt.Sprintf("probe %q duplicates json_equals_baseline field %q", probe.Name, field))
			continue
		}
		seenFields[field] = struct{}{}
	}
	if probe.Assert.JSONSchema != "" {
		path := probe.Assert.JSONSchema
		if !filepath.IsAbs(path) {
			path = filepath.Join(opts.ConfigDir, path)
		}
		if _, err := os.Stat(path); err != nil {
			messages = append(messages, fmt.Sprintf("probe %q json_schema %q does not exist", probe.Name, probe.Assert.JSONSchema))
		}
	}
	return messages
}

func validateCommand(name string, command CommandSpec, allowUnsafe bool) []string {
	var messages []string
	hasArgv := len(command.Argv) > 0
	hasShell := command.Shell != ""
	if hasArgv == hasShell {
		messages = append(messages, fmt.Sprintf("%s command must contain exactly one of argv or shell", name))
	}
	if hasShell && !allowUnsafe {
		messages = append(messages, fmt.Sprintf("%s shell command requires --allow-unsafe-commands", name))
	}
	return messages
}

func hasDisposableProfile(profiles []Profile) bool {
	for _, profile := range profiles {
		if profile.Disposable {
			return true
		}
	}
	return false
}

func validateDeletePattern(pattern string, minPrefix int) error {
	if pattern == "" || pattern == "*" {
		return fmt.Errorf("unsafe delete pattern %q", pattern)
	}
	wildcard := strings.IndexAny(pattern, "*?[")
	if wildcard < 0 {
		if len(pattern) < minPrefix {
			return fmt.Errorf("delete pattern %q has too short literal prefix", pattern)
		}
		return nil
	}
	prefix := pattern[:wildcard]
	if len(prefix) < minPrefix {
		return fmt.Errorf("delete pattern %q has too short literal prefix", pattern)
	}
	return nil
}
