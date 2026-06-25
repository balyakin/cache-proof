package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cacheproof/internal/appx"
	"cacheproof/internal/resp"
	"cacheproof/internal/testutil"

	"github.com/stretchr/testify/require"
)

func TestRunDoctorPrintsNoRedisTraffic(t *testing.T) {
	fakeRedis := testutil.StartRedisFake(t)
	proxyAddr := freeAddr(t)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer app.Close()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cacheproof.yml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`version: 1
proxy:
  listen: %q
  upstream: %q
probe_timeout: "2s"
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
  - name: health
    http:
      method: GET
      url: %q
      headers: {}
      body: ""
    assert:
      expect: pass
      status: 200
`, proxyAddr, fakeRedis.Addr, app.URL)), 0o644))

	var stdout, stderr bytes.Buffer
	err := runDoctor(context.Background(), &stdout, &stderr, &doctorOptions{configPath: cfgPath})
	require.Error(t, err)
	require.True(t, errors.Is(err, appx.ErrProbeFailed))
	require.Contains(t, stdout.String(), "no-redis-traffic FAIL")
	require.Contains(t, stdout.String(), "doctor          FAIL")
}

func TestRunDoctorPassesWithRedisTraffic(t *testing.T) {
	fakeRedis := testutil.StartRedisFake(t)
	proxyAddr := freeAddr(t)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := resp.DialContext(r.Context(), proxyAddr, resp.Auth{})
		if err != nil {
			http.Error(w, "redis unavailable", http.StatusInternalServerError)
			return
		}
		defer client.Close()
		_, err = client.Do(r.Context(), "GET", "product:42")
		if err != nil {
			http.Error(w, "redis unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer app.Close()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cacheproof.yml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`version: 1
proxy:
  listen: %q
  upstream: %q
probe_timeout: "2s"
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
  - name: health
    http:
      method: GET
      url: %q
      headers: {}
      body: ""
    assert:
      expect: pass
      status: 200
`, proxyAddr, fakeRedis.Addr, app.URL)), 0o644))

	var stdout, stderr bytes.Buffer
	err := runDoctor(context.Background(), &stdout, &stderr, &doctorOptions{configPath: cfgPath})
	require.NoError(t, err)
	require.NotContains(t, stdout.String(), "no-redis-traffic FAIL")
	require.Contains(t, stdout.String(), "doctor          PASS")
}
