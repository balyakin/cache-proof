package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"cacheproof/internal/policy"
	cacheproof "cacheproof/pkg/cacheproof"

	"github.com/stretchr/testify/require"
)

func TestBuildAndTerminalReport(t *testing.T) {
	result := BuildRunResult([]ScenarioInput{
		{Name: "baseline", Probes: []ProbeInput{{Name: "catalog", Passed: true, Latency: 5 * time.Millisecond}}},
		{Name: "redis-down", Probes: []ProbeInput{{Name: "checkout", Passed: false, Failures: []string{"expected status 200, got 500"}}}},
	}, []policy.Finding{{Name: "ttl-policy", Level: policy.WARN, Message: `1 keys matching "product:*" have no expiration`}})
	require.False(t, result.Disposable)
	require.Equal(t, 1, result.Summary.Failed)
	require.Equal(t, 1, result.Summary.Warnings)
	require.Equal(t, 1, result.Summary.Passed)

	var out bytes.Buffer
	require.NoError(t, WriteTerminal(&out, result))
	require.Contains(t, out.String(), "baseline        PASS   1 probes passed")
	require.Contains(t, out.String(), "redis-down      FAIL   checkout: expected status 200, got 500")
	require.Contains(t, out.String(), "Cache disposability: FAILED")
}

func TestJSONReport(t *testing.T) {
	result := BuildRunResult([]ScenarioInput{{Name: "baseline", Probes: []ProbeInput{{Name: "catalog", Passed: true}}}}, nil)
	var out bytes.Buffer
	require.NoError(t, WriteJSON(&out, result))
	require.Contains(t, out.String(), `"disposable": true`)
	require.NotContains(t, out.String(), "SECRET_VALUE_SHOULD_NOT_LEAK")
}

func TestJUnitWarnOnlyFailsWithFailOnWarn(t *testing.T) {
	result := BuildRunResult([]ScenarioInput{{Name: "baseline", Probes: []ProbeInput{{Name: "catalog", Passed: true}}}}, []policy.Finding{
		{Name: "ttl-policy", Level: policy.WARN, Message: "warning"},
	})
	var out bytes.Buffer
	require.NoError(t, WriteJUnit(&out, result, "fail"))
	require.NotContains(t, out.String(), `finding/ttl-policy`)

	out.Reset()
	require.NoError(t, WriteJUnit(&out, result, "warn"))
	require.Contains(t, out.String(), `finding/ttl-policy`)
	require.True(t, strings.Contains(out.String(), `<failure`))
}

func TestExitCodeForResult(t *testing.T) {
	require.Equal(t, 0, ExitCodeForResult(RunResult{Disposable: true}, "fail"))
	require.Equal(t, 1, ExitCodeForResult(RunResult{Summary: cacheproof.Summary{Failed: 1}}, "fail"))
	require.Equal(t, 1, ExitCodeForResult(RunResult{Summary: cacheproof.Summary{Warnings: 1}}, "warn"))
}
