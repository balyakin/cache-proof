package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version            int          `yaml:"version" json:"version" validate:"required,oneof=1"`
	Proxy              ProxyConfig  `yaml:"proxy" json:"proxy" validate:"required"`
	Seed               uint64       `yaml:"seed" json:"seed"`
	ProbeTimeout       Duration     `yaml:"probe_timeout" json:"probe_timeout" validate:"required"`
	WarmupRetries      int          `yaml:"warmup_retries" json:"warmup_retries" validate:"min=0,max=10"`
	WarmupDelay        Duration     `yaml:"warmup_delay" json:"warmup_delay"`
	Safety             SafetyConfig `yaml:"safety" json:"safety" validate:"required"`
	Profiles           []Profile    `yaml:"profiles" json:"profiles" validate:"dive"`
	SuspiciousCommands []string     `yaml:"suspicious_commands" json:"suspicious_commands" validate:"dive,required"`
	MaxValueBytes      int          `yaml:"max_value_bytes" json:"max_value_bytes" validate:"required,min=1"`
	MaxKeysScanned     int          `yaml:"max_keys_scanned" json:"max_keys_scanned" validate:"required,min=1"`
	ResetCommand       *CommandSpec `yaml:"reset_command" json:"reset_command,omitempty"`
	Scenarios          []Scenario   `yaml:"scenarios" json:"scenarios" validate:"dive"`
	Probes             []Probe      `yaml:"probes" json:"probes" validate:"required,min=1,dive"`
}

type ProxyConfig struct {
	Listen              string `yaml:"listen" json:"listen" validate:"required,hostname_port"`
	Upstream            string `yaml:"upstream" json:"upstream" validate:"required,hostname_port"`
	UpstreamUsernameEnv string `yaml:"upstream_username_env" json:"upstream_username_env"`
	UpstreamPasswordEnv string `yaml:"upstream_password_env" json:"upstream_password_env"`
}

type SafetyConfig struct {
	AllowKeyDeletes bool `yaml:"allow_key_deletes" json:"allow_key_deletes"`
	MaxKeysDelete   int  `yaml:"max_keys_delete" json:"max_keys_delete" validate:"required,min=1"`
	MinDeletePrefix int  `yaml:"min_delete_prefix" json:"min_delete_prefix" validate:"required,min=1"`
}

type Profile struct {
	Name        string   `yaml:"name" json:"name" validate:"required"`
	KeyPatterns []string `yaml:"key_patterns" json:"key_patterns" validate:"required,min=1,dive,required"`
	MustExpire  bool     `yaml:"must_expire" json:"must_expire"`
	MaxTTL      Duration `yaml:"max_ttl" json:"max_ttl"`
	Disposable  bool     `yaml:"disposable" json:"disposable"`
}

type Scenario struct {
	Name        string  `yaml:"name" json:"name" validate:"required"`
	Type        string  `yaml:"type" json:"type" validate:"required,oneof=random_miss redis_unavailable cold_cache"`
	Probability float64 `yaml:"probability" json:"probability" validate:"min=0,max=1"`
}

type Probe struct {
	Name    string       `yaml:"name" json:"name" validate:"required"`
	HTTP    *HTTPProbe   `yaml:"http" json:"http,omitempty"`
	Command *CommandSpec `yaml:"command" json:"command,omitempty"`
	Assert  Assertion    `yaml:"assert" json:"assert" validate:"required"`
}

type HTTPProbe struct {
	Method  string            `yaml:"method" json:"method" validate:"required,oneof=GET POST PUT PATCH DELETE"`
	URL     string            `yaml:"url" json:"url" validate:"required,url"`
	Headers map[string]string `yaml:"headers" json:"headers"`
	Body    string            `yaml:"body" json:"body"`
}

type CommandSpec struct {
	Argv  []string `yaml:"argv" json:"argv,omitempty"`
	Shell string   `yaml:"shell" json:"shell,omitempty"`
}

type Assertion struct {
	Expect             string   `yaml:"expect" json:"expect" validate:"omitempty,oneof=pass fail"`
	Status             int      `yaml:"status" json:"status" validate:"omitempty,min=100,max=599"`
	JSONSchema         string   `yaml:"json_schema" json:"json_schema"`
	JSONEqualsBaseline []string `yaml:"json_equals_baseline" json:"json_equals_baseline" validate:"dive,required"`
	MaxLatencyIncrease float64  `yaml:"max_latency_increase" json:"max_latency_increase" validate:"min=0"`
	ExitCode           *int     `yaml:"exit_code" json:"exit_code,omitempty" validate:"omitempty,min=0,max=255"`
}

type Duration time.Duration

func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("decode duration: %w", err)
	}
	if raw == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", raw, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	if d == 0 {
		return "", nil
	}
	return time.Duration(d).String(), nil
}

type LoadOptions struct {
	AllowUnsafeCommands bool
	ConfigDir           string
}

type ValidationError struct {
	Messages []string
}

func (e *ValidationError) Error() string {
	return strings.Join(e.Messages, "\n")
}
