package policy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"cacheproof/internal/config"
	"cacheproof/internal/resp"
)

func DeleteColdCache(ctx context.Context, cfg *config.Config, upstream string, auth resp.Auth, allowRemoteDestructive bool, logger *slog.Logger) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !cfg.Safety.AllowKeyDeletes {
		return 0, fmt.Errorf("cold_cache requires safety.allow_key_deletes: true")
	}
	if !allowRemoteDestructive {
		loopback, err := isLoopbackAddr(upstream)
		if err != nil {
			return 0, err
		}
		if !loopback {
			return 0, fmt.Errorf("cold_cache refuses non-loopback upstream %q without --allow-remote-destructive", upstream)
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	client, err := resp.DialContext(ctx, upstream, auth)
	if err != nil {
		return 0, fmt.Errorf("dial redis for cold_cache: %w", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			logger.Debug("close cold_cache redis client", "error", closeErr)
		}
	}()

	deleted := 0
	for _, profile := range cfg.Profiles {
		if !profile.Disposable {
			continue
		}
		for _, pattern := range profile.KeyPatterns {
			if err := validateDeletePattern(pattern, cfg.Safety.MinDeletePrefix); err != nil {
				return deleted, err
			}
			count, err := deletePattern(ctx, client, pattern, cfg.Safety.MaxKeysDelete-deleted)
			if err != nil {
				return deleted, fmt.Errorf("delete cold cache pattern %q: %w", pattern, err)
			}
			deleted += count
			logger.Debug("deleted cold cache keys", "pattern", pattern, "count", count)
			if deleted >= cfg.Safety.MaxKeysDelete {
				return deleted, nil
			}
		}
	}
	return deleted, nil
}

func deletePattern(ctx context.Context, client *resp.Client, pattern string, remaining int) (int, error) {
	deleted := 0
	cursor := "0"
	useDel := false
	for remaining > 0 {
		value, err := client.Do(ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", "100")
		if err != nil {
			return deleted, err
		}
		next, keys, err := parseScanResult(value)
		if err != nil {
			return deleted, err
		}
		cursor = next
		for _, key := range keys {
			if remaining <= 0 {
				break
			}
			command := "UNLINK"
			if useDel {
				command = "DEL"
			}
			if _, err := client.Do(ctx, command, key); err != nil {
				if command == "UNLINK" && resp.IsUnknownCommand(err) {
					useDel = true
					if _, err := client.Do(ctx, "DEL", key); err != nil {
						return deleted, err
					}
				} else {
					return deleted, err
				}
			}
			deleted++
			remaining--
		}
		if cursor == "0" {
			break
		}
	}
	return deleted, nil
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

func isLoopbackAddr(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("parse upstream address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback(), nil
	}
	if host != "localhost" {
		return false, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false, fmt.Errorf("resolve localhost: %w", err)
	}
	if len(ips) == 0 {
		return false, nil
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return false, nil
		}
	}
	return true, nil
}
