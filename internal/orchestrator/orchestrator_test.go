package orchestrator

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"cacheproof/internal/appx"
	"cacheproof/internal/config"
	"cacheproof/internal/fault"
	"cacheproof/internal/policy"
	"cacheproof/internal/probe"
	"cacheproof/internal/recorder"
	"cacheproof/internal/resp"
	"cacheproof/internal/testutil"
	cacheproof "cacheproof/pkg/cacheproof"

	"github.com/stretchr/testify/require"
)

func TestRunBaselineFirstAndOnlyKeepsBaseline(t *testing.T) {
	fakeRedis := testutil.StartRedisFake(t)
	proxyFake := &fakeManagedProxy{}
	runner := &fakeProbeRunner{}
	cfg := orchestratorTestConfig(fakeRedis.Addr)
	cfg.Scenarios = []config.Scenario{
		{Name: "random", Type: "random_miss", Probability: 1},
		{Name: "down", Type: "redis_unavailable"},
	}
	orch := New(func(string, string, resp.Auth, *recorder.Recorder, *slog.Logger) ManagedProxy {
		return proxyFake
	}, runner, nil)

	result, err := orch.Run(context.Background(), RunRequest{Config: cfg, OnlyScenario: "down", NoWarmup: true})
	require.NoError(t, err)
	require.Len(t, result.Scenarios, 2)
	require.Equal(t, "baseline", result.Scenarios[0].Name)
	require.Equal(t, "down", result.Scenarios[1].Name)
	require.Equal(t, []string{"baseline", "down"}, proxyFake.scenarios)
	require.Contains(t, findingNames(result.Findings), "no-redis-traffic")
}

func TestRunReturnsInfrastructureWhenUpstreamDown(t *testing.T) {
	cfg := orchestratorTestConfig("127.0.0.1:1")
	_, err := New(nil, &fakeProbeRunner{}, nil).Run(context.Background(), RunRequest{Config: cfg, NoWarmup: true})
	require.Error(t, err)
	require.True(t, errors.Is(err, appx.ErrInfrastructure))
}

func TestRunUsesInjectedAuditor(t *testing.T) {
	fakeRedis := testutil.StartRedisFake(t)
	proxyFake := &fakeManagedProxy{}
	auditor := &fakeAuditor{findings: []policy.Finding{{Name: "audit", Level: policy.PASS, Message: "mock audit"}}}
	cfg := orchestratorTestConfig(fakeRedis.Addr)
	orch := NewWithAuditor(func(string, string, resp.Auth, *recorder.Recorder, *slog.Logger) ManagedProxy {
		return proxyFake
	}, &fakeProbeRunner{}, auditor, nil)

	result, err := orch.Run(context.Background(), RunRequest{Config: cfg, NoWarmup: true})
	require.NoError(t, err)
	require.True(t, auditor.called)
	require.Contains(t, findingNames(result.Findings), "audit")
}

func TestWarmupRetryAndResetCommand(t *testing.T) {
	runner := &flakyProbeRunner{failuresLeft: 1}
	cfg := orchestratorTestConfig("127.0.0.1:6379")
	cfg.WarmupRetries = 1
	cfg.WarmupDelay = 0
	orch := &orchestrator{probeRunner: runner}
	require.NoError(t, orch.runWarmup(context.Background(), cfg))
	require.Equal(t, 2, runner.calls)

	require.NoError(t, runResetCommand(context.Background(), config.CommandSpec{Argv: []string{"sh", "-c", "exit 0"}}, time.Second))
	require.Error(t, runResetCommand(context.Background(), config.CommandSpec{Argv: []string{"sh", "-c", "exit 2"}}, time.Second))
	require.Error(t, runResetCommand(context.Background(), config.CommandSpec{Argv: []string{"sh", "-c", "sleep 1"}}, time.Millisecond))
}

func TestRunScenarioSetsScenarioBeforeWarmup(t *testing.T) {
	fakeRedis := testutil.StartRedisFake(t)
	proxyFake := &fakeManagedProxy{}
	runner := &scenarioAwareProbeRunner{proxy: proxyFake, want: "random"}
	cfg := orchestratorTestConfig(fakeRedis.Addr)
	orch := &orchestrator{probeRunner: runner}

	_, err := orch.runScenario(context.Background(), cfg, config.Scenario{Name: "random", Type: "random_miss", Probability: 1}, resp.Auth{}, proxyFake, nil, RunRequest{})
	require.NoError(t, err)
	require.True(t, runner.sawScenario)
}

func TestPrepareScenarioPaths(t *testing.T) {
	fakeRedis := testutil.StartRedisFake(t)
	cfg := orchestratorTestConfig(fakeRedis.Addr)
	cfg.Safety.AllowKeyDeletes = true
	proxyFake := &fakeManagedProxy{}
	orch := &orchestrator{}
	require.NoError(t, orch.prepareScenario(context.Background(), cfg, config.Scenario{Name: "random", Type: "random_miss", Probability: 1}, resp.Auth{}, proxyFake, false))
	require.NoError(t, orch.prepareScenario(context.Background(), cfg, config.Scenario{Name: "down", Type: "redis_unavailable"}, resp.Auth{}, proxyFake, false))
	require.NoError(t, orch.prepareScenario(context.Background(), cfg, config.Scenario{Name: "cold", Type: "cold_cache"}, resp.Auth{}, proxyFake, false))
	require.Error(t, orch.prepareScenario(context.Background(), cfg, config.Scenario{Name: "bad", Type: "bad"}, resp.Auth{}, proxyFake, false))
	require.Contains(t, proxyFake.engines, "random-miss")
	require.Contains(t, proxyFake.engines, "redis-unavailable")
}

func orchestratorTestConfig(upstream string) *config.Config {
	return &config.Config{
		Version:      1,
		Seed:         1,
		ProbeTimeout: config.Duration(time.Second),
		Proxy: config.ProxyConfig{
			Listen:   "127.0.0.1:0",
			Upstream: upstream,
		},
		Safety:             config.SafetyConfig{MaxKeysDelete: 10, MinDeletePrefix: 3},
		Profiles:           []config.Profile{{Name: "cache", KeyPatterns: []string{"product:*"}, MustExpire: true, Disposable: true}},
		SuspiciousCommands: config.DefaultSuspicious(),
		MaxValueBytes:      1024,
		MaxKeysScanned:     10,
		Probes: []config.Probe{{
			Name:   "catalog",
			HTTP:   &config.HTTPProbe{Method: "GET", URL: "http://127.0.0.1/probe"},
			Assert: config.Assertion{Expect: "pass", Status: 200},
		}},
	}
}

type fakeProbeRunner struct {
	calls []string
}

func (r *fakeProbeRunner) Run(ctx context.Context, cfgProbe config.Probe, baseline *probe.Result) (probe.Result, error) {
	r.calls = append(r.calls, cfgProbe.Name)
	return probe.Result{ProbeName: cfgProbe.Name, Passed: true, Status: 200, Latency: time.Millisecond}, nil
}

type fakeManagedProxy struct {
	scenarios []string
	engines   []string
}

func (p *fakeManagedProxy) Start(ctx context.Context) error    { return nil }
func (p *fakeManagedProxy) Shutdown(ctx context.Context) error { return nil }
func (p *fakeManagedProxy) SetEngine(engine fault.Engine) {
	p.engines = append(p.engines, engine.Name())
}
func (p *fakeManagedProxy) SetScenario(name string) { p.scenarios = append(p.scenarios, name) }
func (p *fakeManagedProxy) CloseAllConns()          {}

type flakyProbeRunner struct {
	calls        int
	failuresLeft int
}

func (r *flakyProbeRunner) Run(ctx context.Context, cfgProbe config.Probe, baseline *probe.Result) (probe.Result, error) {
	r.calls++
	if r.failuresLeft > 0 {
		r.failuresLeft--
		return probe.Result{}, errors.New("temporary")
	}
	return probe.Result{ProbeName: cfgProbe.Name, Passed: true}, nil
}

type scenarioAwareProbeRunner struct {
	proxy       *fakeManagedProxy
	want        string
	sawScenario bool
}

func (r *scenarioAwareProbeRunner) Run(ctx context.Context, cfgProbe config.Probe, baseline *probe.Result) (probe.Result, error) {
	if len(r.proxy.scenarios) > 0 && r.proxy.scenarios[len(r.proxy.scenarios)-1] == r.want {
		r.sawScenario = true
	}
	return probe.Result{ProbeName: cfgProbe.Name, Passed: true}, nil
}

type fakeAuditor struct {
	called   bool
	findings []policy.Finding
	err      error
}

func (a *fakeAuditor) Audit(ctx context.Context, upstream string, auth resp.Auth, snapshot recorder.Snapshot) ([]policy.Finding, error) {
	a.called = true
	if a.err != nil {
		return nil, a.err
	}
	return a.findings, nil
}

func findingNames(findings []cacheproof.Finding) []string {
	names := make([]string, 0, len(findings))
	for _, finding := range findings {
		names = append(names, finding.Name)
	}
	return names
}
