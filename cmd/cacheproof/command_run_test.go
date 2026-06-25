package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cacheproof/internal/appx"
	"cacheproof/internal/resp"
	"cacheproof/internal/testutil"
	cacheproof "cacheproof/pkg/cacheproof"

	"github.com/stretchr/testify/require"
)

func TestRunTestEndToEndPassesAndWritesReports(t *testing.T) {
	fakeRedis := testutil.StartRedisFake(t)
	const secret = "SECRET_VALUE_SHOULD_NOT_LEAK"
	productBody := fmt.Sprintf(`{"id":42,"name":"Demo","price":19.99,"secret":%q}`, secret)
	fakeRedis.Handler = func(cmd *resp.Command) []byte {
		switch cmd.Name {
		case "PING":
			return []byte("+PONG\r\n")
		case "GET":
			return bulkString(productBody)
		case "SETEX", "SET":
			return []byte("+OK\r\n")
		case "SCAN":
			return []byte("*2\r\n$1\r\n0\r\n*0\r\n")
		case "TTL":
			return []byte(":-2\r\n")
		default:
			return []byte("+OK\r\n")
		}
	}
	proxyAddr := freeAddr(t)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := resp.DialContext(r.Context(), proxyAddr, resp.Auth{})
		if err == nil {
			defer client.Close()
			value, err := client.Do(r.Context(), "GET", "product:42")
			if raw, ok := value.(string); err == nil && ok && raw != "" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(raw))
				return
			}
			_, _ = client.Do(r.Context(), "SETEX", "product:42", "60", productBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(productBody))
	}))
	defer app.Close()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "schemas"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schemas", "product.json"), []byte(`{"type":"object","required":["id","name","price"]}`), 0o644))
	cfgPath := filepath.Join(dir, "cacheproof.yml")
	jsonPath := filepath.Join(dir, "report.json")
	junitPath := filepath.Join(dir, "report.xml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`version: 1
proxy:
  listen: %q
  upstream: %q
seed: 1
probe_timeout: "3s"
warmup_retries: 0
warmup_delay: "1ms"
safety:
  allow_key_deletes: false
  max_keys_delete: 10
  min_delete_prefix: 3
profiles:
  - name: cache
    key_patterns: ["product:*"]
    must_expire: true
    max_ttl: "24h"
    disposable: true
suspicious_commands: [XADD, XREAD, XRANGE, LPUSH, RPUSH, BLPOP, BRPOP, PUBLISH, SUBSCRIBE, PSUBSCRIBE, PERSIST]
max_value_bytes: 1048576
max_keys_scanned: 100
scenarios:
  - name: random-miss
    type: random_miss
    probability: 1
probes:
  - name: catalog
    http:
      method: GET
      url: %q
      headers: {}
      body: ""
    assert:
      expect: pass
      status: 200
      json_schema: "schemas/product.json"
      json_equals_baseline: ["id", "name", "price"]
  - name: secret-command
    command:
      argv: ["sh", "-c", "printf SECRET_VALUE_SHOULD_NOT_LEAK"]
    assert:
      expect: pass
      exit_code: 0
`, proxyAddr, fakeRedis.Addr, app.URL)), 0o644))

	var stdout, stderr bytes.Buffer
	err := runTest(context.Background(), &stdout, &stderr, &testOptions{
		configPath:  cfgPath,
		reportJSON:  jsonPath,
		reportJUnit: junitPath,
		failOn:      "fail",
		noWarmup:    true,
	})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "baseline")
	require.FileExists(t, jsonPath)
	require.FileExists(t, junitPath)
	rawJSON, err := os.ReadFile(jsonPath)
	require.NoError(t, err)
	require.Contains(t, string(rawJSON), `"disposable": true`)
	require.NotContains(t, string(rawJSON), "SECRET_VALUE_SHOULD_NOT_LEAK")
	require.NotContains(t, stdout.String(), "SECRET_VALUE_SHOULD_NOT_LEAK")
	require.NotContains(t, stderr.String(), "SECRET_VALUE_SHOULD_NOT_LEAK")
	rawJUnit, err := os.ReadFile(junitPath)
	require.NoError(t, err)
	require.NotContains(t, string(rawJUnit), "SECRET_VALUE_SHOULD_NOT_LEAK")
}

func TestRunTestRedisUnavailableDetectsCheckoutFailure(t *testing.T) {
	fakeRedis := testutil.StartRedisFake(t)
	fakeRedis.Handler = func(cmd *resp.Command) []byte {
		switch cmd.Name {
		case "PING":
			return []byte("+PONG\r\n")
		case "GET":
			if len(cmd.Args) > 1 && cmd.Args[1] == "cart:7" {
				return bulkString(`{"items":[1]}`)
			}
			return []byte("$-1\r\n")
		case "SCAN":
			return []byte("*2\r\n$1\r\n0\r\n*0\r\n")
		case "TTL":
			return []byte(":-2\r\n")
		default:
			return []byte("+OK\r\n")
		}
	}
	proxyAddr := freeAddr(t)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := resp.DialContext(r.Context(), proxyAddr, resp.Auth{})
		if err != nil {
			http.Error(w, "checkout unavailable", http.StatusInternalServerError)
			return
		}
		defer client.Close()
		value, err := client.Do(r.Context(), "GET", "cart:7")
		if err != nil || value == nil {
			http.Error(w, "cart missing from cache", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer app.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cacheproof.yml")
	jsonPath := filepath.Join(dir, "report.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`version: 1
proxy:
  listen: %q
  upstream: %q
seed: 1
probe_timeout: "3s"
warmup_retries: 0
warmup_delay: "1ms"
safety:
  allow_key_deletes: false
  max_keys_delete: 10
  min_delete_prefix: 3
profiles:
  - name: cache
    key_patterns: ["cart:*"]
    must_expire: true
    disposable: true
suspicious_commands: [XADD, XREAD, XRANGE, LPUSH, RPUSH, BLPOP, BRPOP, PUBLISH, SUBSCRIBE, PSUBSCRIBE, PERSIST]
max_value_bytes: 1048576
max_keys_scanned: 100
scenarios:
  - name: redis-down
    type: redis_unavailable
probes:
  - name: checkout
    http:
      method: POST
      url: %q
      headers:
        Content-Type: "application/json"
      body: '{"cart_id":7}'
    assert:
      expect: pass
      status: 200
`, proxyAddr, fakeRedis.Addr, app.URL)), 0o644))

	var stdout, stderr bytes.Buffer
	err := runTest(context.Background(), &stdout, &stderr, &testOptions{
		configPath: cfgPath,
		reportJSON: jsonPath,
		failOn:     "fail",
		noWarmup:   true,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, appx.ErrProbeFailed))
	require.FileExists(t, jsonPath)
	rawJSON, err := os.ReadFile(jsonPath)
	require.NoError(t, err)
	var result cacheproof.RunResult
	require.NoError(t, json.Unmarshal(rawJSON, &result))
	require.True(t, result.Scenarios[0].Passed)
	require.Equal(t, "redis-down", result.Scenarios[1].Name)
	require.False(t, result.Scenarios[1].Probes[0].Passed)
	require.Contains(t, stdout.String(), "redis-down")
	require.Empty(t, stderr.String())
}

func TestRootLoggerVersionAndRunID(t *testing.T) {
	var out bytes.Buffer
	root := newRootCommand(context.Background(), &out, &bytes.Buffer{})
	require.NotNil(t, root)
	logger := NewLogger(true, &bytes.Buffer{})
	require.NotNil(t, logger)
	runID, err := newRunID()
	require.NoError(t, err)
	require.Len(t, runID, 32)
	cmd := newVersionCommand(&out)
	require.NoError(t, cmd.Execute())
	require.Contains(t, out.String(), version)
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	return addr
}

func bulkString(value string) []byte {
	return []byte(fmt.Sprintf("$%d\r\n%s\r\n", len(value), value))
}
