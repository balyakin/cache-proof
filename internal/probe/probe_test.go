package probe

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cacheproof/internal/config"
	"cacheproof/internal/testutil"

	"github.com/stretchr/testify/require"
)

func TestHTTPStatusSchemaAndBaselineEquality(t *testing.T) {
	server := testutil.JSONServer(http.StatusOK, `{"id":42,"name":"Demo","price":19.99}`)
	defer server.Close()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "product.json"), []byte(`{"type":"object","required":["id","name"]}`), 0o644))
	cfg := &config.Config{Probes: []config.Probe{{
		Name: "catalog",
		HTTP: &config.HTTPProbe{Method: "GET", URL: server.URL},
		Assert: config.Assertion{
			Expect:             "pass",
			Status:             200,
			JSONSchema:         "product.json",
			JSONEqualsBaseline: []string{"id", "name"},
		},
	}}}
	schemas, err := CompileSchemas(context.Background(), cfg, dir)
	require.NoError(t, err)
	runner := NewRunner(server.Client(), false, schemas, nil)

	baseline, err := runner.Run(context.Background(), cfg.Probes[0], nil)
	require.NoError(t, err)
	require.True(t, baseline.Passed)
	result, err := runner.Run(context.Background(), cfg.Probes[0], &baseline)
	require.NoError(t, err)
	require.True(t, result.Passed, result.Failures)
}

func TestHTTPStatusFailureAndExpectFail(t *testing.T) {
	server := testutil.JSONServer(http.StatusInternalServerError, `{"error":"broken"}`)
	defer server.Close()
	cfgProbe := config.Probe{
		Name:   "checkout",
		HTTP:   &config.HTTPProbe{Method: "GET", URL: server.URL},
		Assert: config.Assertion{Expect: "fail", Status: 200},
	}
	result, err := NewRunner(server.Client(), false, nil, nil).Run(context.Background(), cfgProbe, nil)
	require.NoError(t, err)
	require.True(t, result.Passed)
	require.Contains(t, result.Failures[0], "expected status 200")
}

func TestCommandArgvExitCode(t *testing.T) {
	exitCode := 0
	cfgProbe := config.Probe{
		Name:    "cmd",
		Command: &config.CommandSpec{Argv: []string{"sh", "-c", "exit 0"}},
		Assert:  config.Assertion{Expect: "pass", ExitCode: &exitCode},
	}
	result, err := NewRunner(nil, false, nil, nil).Run(context.Background(), cfgProbe, nil)
	require.NoError(t, err)
	require.True(t, result.Passed, result.Failures)
}

func TestShellRejectedWithoutFlag(t *testing.T) {
	exitCode := 0
	cfgProbe := config.Probe{
		Name:    "shell",
		Command: &config.CommandSpec{Shell: "exit 0"},
		Assert:  config.Assertion{Expect: "pass", ExitCode: &exitCode},
	}
	result, err := NewRunner(nil, false, nil, nil).Run(context.Background(), cfgProbe, nil)
	require.NoError(t, err)
	require.False(t, result.Passed)
	require.Contains(t, result.Failures[0], "allow-unsafe-commands")
}

func TestCommandTimeout(t *testing.T) {
	exitCode := 0
	cfgProbe := config.Probe{
		Name:    "timeout",
		Command: &config.CommandSpec{Argv: []string{"sh", "-c", "sleep 1"}},
		Assert:  config.Assertion{Expect: "pass", ExitCode: &exitCode},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	result, err := NewRunner(nil, false, nil, nil).Run(ctx, cfgProbe, nil)
	require.Error(t, err)
	require.False(t, result.Passed)
	require.Contains(t, result.Failures[0], "probe timed out")
}

func TestHTTPInfrastructureErrorReturnsError(t *testing.T) {
	cfgProbe := config.Probe{
		Name:   "unavailable",
		HTTP:   &config.HTTPProbe{Method: "GET", URL: closedHTTPURL(t)},
		Assert: config.Assertion{Expect: "pass", Status: 200},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := NewRunner(nil, false, nil, nil).Run(ctx, cfgProbe, nil)
	require.Error(t, err)
	require.False(t, result.Passed)
	require.Contains(t, result.Failures[0], "http request failed")
}

func TestHTTPResponseBodyLimit(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := io.NopCloser(io.LimitReader(repeatingReader{}, 64*1024*1024+1))
		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
		}
		return response, nil
	})}
	cfgProbe := config.Probe{
		Name:   "large",
		HTTP:   &config.HTTPProbe{Method: "GET", URL: "http://cacheproof.test/large"},
		Assert: config.Assertion{Expect: "pass", Status: http.StatusOK},
	}

	result, err := NewRunner(client, false, nil, nil).Run(context.Background(), cfgProbe, nil)
	require.NoError(t, err)
	require.False(t, result.Passed)
	require.Contains(t, result.Failures[0], "http response body exceeds maximum size")
	require.Empty(t, result.Body)
}

func TestSchemaAndBaselineFailures(t *testing.T) {
	server := testutil.JSONServer(http.StatusOK, `{"id":43,"name":"Other"}`)
	defer server.Close()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.json"), []byte(`{"type":"object","required":["price"]}`), 0o644))
	cfg := &config.Config{Probes: []config.Probe{{
		Name: "p",
		HTTP: &config.HTTPProbe{Method: "GET", URL: server.URL},
		Assert: config.Assertion{
			Expect:             "pass",
			Status:             200,
			JSONSchema:         "schema.json",
			JSONEqualsBaseline: []string{"id", "missing"},
		},
	}}}
	schemas, err := CompileSchemas(context.Background(), cfg, dir)
	require.NoError(t, err)
	baseline := &Result{Body: []byte(`{"id":42}`), Latency: time.Millisecond}
	result, err := NewRunner(server.Client(), false, schemas, nil).Run(context.Background(), cfg.Probes[0], baseline)
	require.NoError(t, err)
	require.False(t, result.Passed)
	require.Contains(t, result.Failures[0], "json schema validation failed")
	require.Contains(t, result.Failures[1], "field id changed")
}

func TestBaselineComparisonRedactsValues(t *testing.T) {
	server := testutil.JSONServer(http.StatusOK, `{"token":"SECRET_VALUE_SHOULD_NOT_LEAK"}`)
	defer server.Close()
	cfgProbe := config.Probe{
		Name: "p",
		HTTP: &config.HTTPProbe{Method: "GET", URL: server.URL},
		Assert: config.Assertion{
			Expect:             "pass",
			Status:             200,
			JSONEqualsBaseline: []string{"token"},
		},
	}
	baseline := &Result{Body: []byte(`{"token":"old"}`), Latency: time.Millisecond}
	result, err := NewRunner(server.Client(), false, nil, nil).Run(context.Background(), cfgProbe, baseline)
	require.NoError(t, err)
	require.False(t, result.Passed)
	require.Contains(t, result.Failures[0], "field token changed vs baseline")
	require.NotContains(t, result.Failures[0], "SECRET_VALUE_SHOULD_NOT_LEAK")
	require.NotContains(t, result.Failures[0], "old")
}

func TestCommandExitCodeFailureAndOutputLimit(t *testing.T) {
	exitCode := 0
	cfgProbe := config.Probe{
		Name:    "cmd",
		Command: &config.CommandSpec{Argv: []string{"sh", "-c", "printf '%070000d' 1; exit 2"}},
		Assert:  config.Assertion{Expect: "pass", ExitCode: &exitCode},
	}
	result, err := NewRunner(nil, false, nil, nil).Run(context.Background(), cfgProbe, nil)
	require.NoError(t, err)
	require.False(t, result.Passed)
	require.Equal(t, maxCommandOutput, len(result.Output))
	require.Contains(t, result.Failures[0], "expected exit code 0, got 2")
}

func TestCompileSchemasCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CompileSchemas(ctx, &config.Config{}, t.TempDir())
	require.Error(t, err)
}

func closedHTTPURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + addr
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type repeatingReader struct{}

func (repeatingReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 'x'
	}
	return len(buffer), nil
}
