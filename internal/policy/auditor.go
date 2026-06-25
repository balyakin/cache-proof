package policy

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"cacheproof/internal/config"
	"cacheproof/internal/recorder"
	"cacheproof/internal/resp"
)

type Auditor interface {
	Audit(ctx context.Context, upstream string, auth resp.Auth, snapshot recorder.Snapshot) ([]Finding, error)
}

type auditor struct {
	cfg    *config.Config
	logger *slog.Logger
}

var _ Auditor = (*auditor)(nil)

func NewAuditor(cfg *config.Config, logger *slog.Logger) Auditor {
	if logger == nil {
		logger = slog.Default()
	}
	return &auditor{cfg: cfg, logger: logger}
}

func (a *auditor) Audit(ctx context.Context, upstream string, auth resp.Auth, snapshot recorder.Snapshot) ([]Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var findings []Finding
	if hasDisposable(a.cfg.Profiles) {
		suspicious := make(map[string]struct{})
		for _, command := range a.cfg.SuspiciousCommands {
			suspicious[strings.ToUpper(command)] = struct{}{}
		}
		commands := make([]string, 0, len(snapshot.CommandCounts))
		for command := range snapshot.CommandCounts {
			commands = append(commands, command)
		}
		sort.Strings(commands)
		for _, command := range commands {
			if _, ok := suspicious[command]; ok {
				findings = append(findings, Finding{
					Name:    "cache-contract",
					Level:   FAIL,
					Message: command + " observed while disposable cache profile exists",
				})
			}
		}
	}
	if snapshot.BigValueCount > 0 {
		findings = append(findings, Finding{
			Name:    "value-size",
			Level:   WARN,
			Message: fmt.Sprintf("%d Redis values exceeded max_value_bytes", snapshot.BigValueCount),
		})
	}

	client, err := resp.DialContext(ctx, upstream, auth)
	if err != nil {
		return nil, fmt.Errorf("dial redis for policy audit: %w", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			a.logger.Debug("close policy redis client", "error", closeErr)
		}
	}()
	ttlFindings, err := a.auditTTL(ctx, client)
	if err != nil {
		return nil, err
	}
	findings = append(findings, ttlFindings...)

	if len(findings) == 0 {
		findings = append(findings, Finding{Name: "audit", Level: PASS, Message: "no cache policy violations found"})
	}
	return findings, nil
}

func (a *auditor) auditTTL(ctx context.Context, client *resp.Client) ([]Finding, error) {
	var findings []Finding
	scanned := 0
	for _, profile := range a.cfg.Profiles {
		for _, pattern := range profile.KeyPatterns {
			noExpiration := 0
			tooLong := 0
			cursor := "0"
			for {
				if scanned >= a.cfg.MaxKeysScanned {
					break
				}
				value, err := client.Do(ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", "100")
				if err != nil {
					return nil, fmt.Errorf("scan redis keys for pattern %q: %w", pattern, err)
				}
				next, keys, err := parseScanResult(value)
				if err != nil {
					return nil, fmt.Errorf("parse scan result for pattern %q: %w", pattern, err)
				}
				cursor = next
				for _, key := range keys {
					if scanned >= a.cfg.MaxKeysScanned {
						break
					}
					scanned++
					ttlRaw, err := client.Do(ctx, "TTL", key)
					if err != nil {
						return nil, fmt.Errorf("ttl redis key for pattern %q: %w", pattern, err)
					}
					ttl, ok := asInt64(ttlRaw)
					if !ok {
						return nil, fmt.Errorf("unexpected TTL reply for pattern %q", pattern)
					}
					if ttl == -2 {
						continue
					}
					if profile.MustExpire && ttl == -1 {
						noExpiration++
						continue
					}
					if profile.MaxTTL.Std() > 0 && ttl > int64(profile.MaxTTL.Std().Seconds()) {
						tooLong++
					}
				}
				if cursor == "0" || scanned >= a.cfg.MaxKeysScanned {
					break
				}
			}
			if noExpiration > 0 {
				findings = append(findings, Finding{
					Name:    "ttl-policy",
					Level:   WARN,
					Message: fmt.Sprintf("%d keys matching %q have no expiration", noExpiration, pattern),
				})
			}
			if tooLong > 0 {
				findings = append(findings, Finding{
					Name:    "ttl-policy",
					Level:   WARN,
					Message: fmt.Sprintf("%d keys matching %q exceed max_ttl", tooLong, pattern),
				})
			}
		}
	}
	return findings, nil
}

func parseScanResult(value interface{}) (string, []string, error) {
	rows, ok := value.([]interface{})
	if !ok || len(rows) != 2 {
		return "", nil, fmt.Errorf("SCAN reply must be two-element array")
	}
	cursor, ok := rows[0].(string)
	if !ok {
		return "", nil, fmt.Errorf("SCAN cursor must be string")
	}
	keyRows, ok := rows[1].([]interface{})
	if !ok {
		return "", nil, fmt.Errorf("SCAN keys must be array")
	}
	keys := make([]string, 0, len(keyRows))
	for _, row := range keyRows {
		key, ok := row.(string)
		if !ok {
			return "", nil, fmt.Errorf("SCAN key must be string")
		}
		keys = append(keys, key)
	}
	return cursor, keys, nil
}

func asInt64(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func hasDisposable(profiles []config.Profile) bool {
	for _, profile := range profiles {
		if profile.Disposable {
			return true
		}
	}
	return false
}
