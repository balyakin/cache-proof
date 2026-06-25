package policy

import (
	"context"
	"testing"
	"time"

	"cacheproof/internal/config"
	"cacheproof/internal/recorder"
	"cacheproof/internal/resp"
	"cacheproof/internal/testutil"

	"github.com/stretchr/testify/require"
)

func TestAuditSuspiciousAndBigValue(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	cfg := policyTestConfig()
	snapshot := recorder.Snapshot{
		CommandCounts: map[string]int{"XADD": 1},
		BigValueCount: 2,
	}
	findings, err := NewAuditor(cfg, nil).Audit(context.Background(), fake.Addr, resp.Auth{}, snapshot)
	require.NoError(t, err)
	require.Contains(t, findingMessages(findings), "XADD observed while disposable cache profile exists")
	require.Contains(t, findingNames(findings), "value-size")
}

func TestAuditTTLWarnings(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	fake.Handler = func(cmd *resp.Command) []byte {
		switch cmd.Name {
		case "PING":
			return []byte("+PONG\r\n")
		case "SCAN":
			return []byte("*2\r\n$1\r\n0\r\n*2\r\n$9\r\nproduct:1\r\n$9\r\nproduct:2\r\n")
		case "TTL":
			if cmd.Args[1] == "product:1" {
				return []byte(":-1\r\n")
			}
			return []byte(":90000\r\n")
		default:
			return []byte("+OK\r\n")
		}
	}
	cfg := policyTestConfig()
	findings, err := NewAuditor(cfg, nil).Audit(context.Background(), fake.Addr, resp.Auth{}, recorder.Snapshot{CommandCounts: map[string]int{}})
	require.NoError(t, err)
	require.Contains(t, findingMessages(findings), `1 keys matching "product:*" have no expiration`)
	require.Contains(t, findingMessages(findings), `1 keys matching "product:*" exceed max_ttl`)
}

func TestColdCacheDeletesWithoutFlush(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	fake.Handler = func(cmd *resp.Command) []byte {
		switch cmd.Name {
		case "PING":
			return []byte("+PONG\r\n")
		case "SCAN":
			return []byte("*2\r\n$1\r\n0\r\n*1\r\n$9\r\nproduct:1\r\n")
		case "UNLINK":
			return []byte(":1\r\n")
		default:
			return []byte("+OK\r\n")
		}
	}
	cfg := policyTestConfig()
	cfg.Safety.AllowKeyDeletes = true
	deleted, err := DeleteColdCache(context.Background(), cfg, fake.Addr, resp.Auth{}, false, nil)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	require.NotContains(t, fake.Commands(), "FLUSHDB")
	require.NotContains(t, fake.Commands(), "FLUSHALL")
}

func TestValidateDeletePatternRejectsUnsafe(t *testing.T) {
	require.Error(t, validateDeletePattern("*", 3))
	require.Error(t, validateDeletePattern("ab*", 3))
	require.NoError(t, validateDeletePattern("abc*", 3))
}

func TestAuditPassWhenNoFindings(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	cfg := policyTestConfig()
	cfg.Profiles[0].MustExpire = false
	findings, err := NewAuditor(cfg, nil).Audit(context.Background(), fake.Addr, resp.Auth{}, recorder.Snapshot{CommandCounts: map[string]int{}})
	require.NoError(t, err)
	require.Equal(t, []Finding{{Name: "audit", Level: PASS, Message: "no cache policy violations found"}}, findings)
}

func TestColdCacheFallbackToDEL(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	fake.Handler = func(cmd *resp.Command) []byte {
		switch cmd.Name {
		case "PING":
			return []byte("+PONG\r\n")
		case "SCAN":
			return []byte("*2\r\n$1\r\n0\r\n*1\r\n$9\r\nproduct:1\r\n")
		case "UNLINK":
			return []byte("-ERR unknown command 'UNLINK'\r\n")
		case "DEL":
			return []byte(":1\r\n")
		default:
			return []byte("+OK\r\n")
		}
	}
	cfg := policyTestConfig()
	cfg.Safety.AllowKeyDeletes = true
	deleted, err := DeleteColdCache(context.Background(), cfg, fake.Addr, resp.Auth{}, false, nil)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	require.Contains(t, fake.Commands(), "DEL")
}

func TestLoopbackAndParsingHelpers(t *testing.T) {
	ok, err := isLoopbackAddr("127.0.0.1:6379")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = isLoopbackAddr("localhost:6379")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = isLoopbackAddr("0.0.0.0:6379")
	require.NoError(t, err)
	require.False(t, ok)
	_, err = isLoopbackAddr("bad")
	require.Error(t, err)

	cursor, keys, err := parseScanResult([]interface{}{"0", []interface{}{"a", "b"}})
	require.NoError(t, err)
	require.Equal(t, "0", cursor)
	require.Equal(t, []string{"a", "b"}, keys)
	_, _, err = parseScanResult([]interface{}{"0", "bad"})
	require.Error(t, err)
	value, ok := asInt64("42")
	require.True(t, ok)
	require.Equal(t, int64(42), value)
	_, ok = asInt64(struct{}{})
	require.False(t, ok)
	require.NotEmpty(t, DefaultSuspicious())
}

func TestColdCacheSafetyErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DeleteColdCache(ctx, policyTestConfig(), "127.0.0.1:6379", resp.Auth{}, false, nil)
	require.Error(t, err)

	cfg := policyTestConfig()
	_, err = DeleteColdCache(context.Background(), cfg, "127.0.0.1:6379", resp.Auth{}, false, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow_key_deletes")

	cfg.Safety.AllowKeyDeletes = true
	_, err = DeleteColdCache(context.Background(), cfg, "192.0.2.1:6379", resp.Auth{}, false, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-loopback")
}

func TestColdCacheInvalidPatternAfterLoopback(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	cfg := policyTestConfig()
	cfg.Safety.AllowKeyDeletes = true
	cfg.Profiles[0].KeyPatterns = []string{"*"}
	_, err := DeleteColdCache(context.Background(), cfg, fake.Addr, resp.Auth{}, false, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsafe delete pattern")
}

func TestScanParsingMoreErrors(t *testing.T) {
	_, _, err := parseScanResult("bad")
	require.Error(t, err)
	_, _, err = parseScanResult([]interface{}{1, []interface{}{}})
	require.Error(t, err)
	_, _, err = parseScanResult([]interface{}{"0", []interface{}{1}})
	require.Error(t, err)
	value, ok := asInt64(7)
	require.True(t, ok)
	require.Equal(t, int64(7), value)
}

func TestDeletePatternLimitAndValidation(t *testing.T) {
	require.Error(t, validateDeletePattern("ab", 3))
	require.NoError(t, validateDeletePattern("abc", 3))
	fake := testutil.StartRedisFake(t)
	fake.Handler = func(cmd *resp.Command) []byte {
		switch cmd.Name {
		case "PING":
			return []byte("+PONG\r\n")
		case "SCAN":
			return []byte("*2\r\n$1\r\n0\r\n*2\r\n$9\r\nproduct:1\r\n$9\r\nproduct:2\r\n")
		case "UNLINK":
			return []byte(":1\r\n")
		default:
			return []byte("+OK\r\n")
		}
	}
	cfg := policyTestConfig()
	cfg.Safety.AllowKeyDeletes = true
	cfg.Safety.MaxKeysDelete = 1
	deleted, err := DeleteColdCache(context.Background(), cfg, fake.Addr, resp.Auth{}, false, nil)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
}

func policyTestConfig() *config.Config {
	return &config.Config{
		Safety: config.SafetyConfig{AllowKeyDeletes: false, MaxKeysDelete: 10, MinDeletePrefix: 3},
		Profiles: []config.Profile{{
			Name:        "cache",
			KeyPatterns: []string{"product:*"},
			MustExpire:  true,
			MaxTTL:      config.Duration(24 * time.Hour),
			Disposable:  true,
		}},
		SuspiciousCommands: config.DefaultSuspicious(),
		MaxKeysScanned:     100,
	}
}

func findingMessages(findings []Finding) []string {
	messages := make([]string, 0, len(findings))
	for _, finding := range findings {
		messages = append(messages, finding.Message)
	}
	return messages
}

func findingNames(findings []Finding) []string {
	names := make([]string, 0, len(findings))
	for _, finding := range findings {
		names = append(names, finding.Name)
	}
	return names
}
