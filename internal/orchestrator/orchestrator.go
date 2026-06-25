package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"cacheproof/internal/appx"
	"cacheproof/internal/config"
	"cacheproof/internal/fault"
	"cacheproof/internal/policy"
	"cacheproof/internal/probe"
	"cacheproof/internal/proxy"
	"cacheproof/internal/recorder"
	"cacheproof/internal/report"
	"cacheproof/internal/resp"
)

type Orchestrator interface {
	Run(ctx context.Context, request RunRequest) (report.RunResult, error)
}

type RunRequest struct {
	Config                 *config.Config
	ConfigPath             string
	OnlyScenario           string
	NoWarmup               bool
	FailOn                 string
	AllowRemoteDestructive bool
}

type ManagedProxy interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
	SetEngine(engine fault.Engine)
	SetScenario(name string)
	CloseAllConns()
}

type ProxyFactory func(listen string, upstream string, auth resp.Auth, rec *recorder.Recorder, logger *slog.Logger) ManagedProxy

type orchestrator struct {
	proxyFactory ProxyFactory
	probeRunner  probe.Runner
	auditor      policy.Auditor
	logger       *slog.Logger
}

var _ Orchestrator = (*orchestrator)(nil)

func New(proxyFactory ProxyFactory, probeRunner probe.Runner, logger *slog.Logger) Orchestrator {
	return NewWithAuditor(proxyFactory, probeRunner, nil, logger)
}

func NewWithAuditor(proxyFactory ProxyFactory, probeRunner probe.Runner, auditor policy.Auditor, logger *slog.Logger) Orchestrator {
	if proxyFactory == nil {
		proxyFactory = func(listen string, upstream string, auth resp.Auth, rec *recorder.Recorder, logger *slog.Logger) ManagedProxy {
			return proxy.New(listen, upstream, auth, rec, logger)
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &orchestrator{proxyFactory: proxyFactory, probeRunner: probeRunner, auditor: auditor, logger: logger}
}

func (o *orchestrator) Run(ctx context.Context, request RunRequest) (report.RunResult, error) {
	if err := ctx.Err(); err != nil {
		return report.RunResult{}, fmt.Errorf("%w: context before run: %v", appx.ErrInfrastructure, err)
	}
	if request.Config == nil {
		return report.RunResult{}, fmt.Errorf("%w: config is required", appx.ErrConfig)
	}
	if o.probeRunner == nil {
		return report.RunResult{}, fmt.Errorf("%w: probe runner is required", appx.ErrInfrastructure)
	}
	cfg := request.Config
	auth := resolveAuth(cfg)
	if err := pingUpstream(ctx, cfg.Proxy.Upstream, auth); err != nil {
		return report.RunResult{}, fmt.Errorf("%w: upstream unavailable: %v", appx.ErrInfrastructure, err)
	}

	rec := recorder.New(cfg.MaxValueBytes)
	cacheProxy := o.proxyFactory(cfg.Proxy.Listen, cfg.Proxy.Upstream, auth, rec, o.logger)
	if err := cacheProxy.Start(ctx); err != nil {
		return report.RunResult{}, fmt.Errorf("%w: start proxy: %v", appx.ErrInfrastructure, err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := cacheProxy.Shutdown(shutdownCtx); err != nil {
			o.logger.Error("shutdown proxy", "error", err)
		}
	}()

	var scenarios []report.ScenarioInput
	var findings []policy.Finding
	if cfg.ResetCommand != nil {
		if err := runResetCommand(ctx, *cfg.ResetCommand, cfg.ProbeTimeout.Std()); err != nil {
			return report.RunResult{}, fmt.Errorf("%w: reset before baseline: %v", appx.ErrInfrastructure, err)
		}
	}
	if !request.NoWarmup {
		if err := o.runWarmup(ctx, cfg); err != nil {
			return report.RunResult{}, fmt.Errorf("%w: warmup before baseline: %v", appx.ErrInfrastructure, err)
		}
	}
	cacheProxy.SetScenario("baseline")
	beforeBaselineRows := rec.Snapshot().ObservedCommandRows
	baselineProbes, baselineByName, err := o.runMeasuredProbes(ctx, cfg, nil)
	if err != nil {
		return report.RunResult{}, err
	}
	scenarios = append(scenarios, report.ScenarioInput{Name: "baseline", Probes: baselineProbes})
	if rec.Snapshot().ObservedCommandRows-beforeBaselineRows == 0 {
		findings = append(findings, policy.Finding{
			Name:    "no-redis-traffic",
			Level:   policy.FAIL,
			Message: "no Redis traffic observed through proxy; the app may connect directly to upstream",
		})
	}

	for _, scenario := range cfg.Scenarios {
		if request.OnlyScenario != "" && scenario.Name != request.OnlyScenario {
			continue
		}
		cacheProxy.SetEngine(fault.PassThrough{})
		scenarioResult, err := o.runScenario(ctx, cfg, scenario, auth, cacheProxy, baselineByName, request)
		cacheProxy.SetEngine(fault.PassThrough{})
		if err != nil {
			return report.RunResult{}, err
		}
		scenarios = append(scenarios, scenarioResult)
	}

	auditor := o.auditor
	if auditor == nil {
		auditor = policy.NewAuditor(cfg, o.logger)
	}
	auditFindings, err := auditor.Audit(ctx, cfg.Proxy.Upstream, auth, rec.Snapshot())
	if err != nil {
		return report.RunResult{}, fmt.Errorf("%w: policy audit: %v", appx.ErrInfrastructure, err)
	}
	findings = append(findings, auditFindings...)
	return report.BuildRunResult(scenarios, findings), nil
}

func resolveAuth(cfg *config.Config) resp.Auth {
	auth := resp.Auth{}
	if cfg.Proxy.UpstreamUsernameEnv != "" {
		auth.Username = os.Getenv(cfg.Proxy.UpstreamUsernameEnv)
	}
	if cfg.Proxy.UpstreamPasswordEnv != "" {
		auth.Password = os.Getenv(cfg.Proxy.UpstreamPasswordEnv)
	}
	return auth
}

func pingUpstream(ctx context.Context, upstream string, auth resp.Auth) error {
	client, err := resp.DialContext(ctx, upstream, auth)
	if err != nil {
		return err
	}
	defer func() {
		_ = client.Close()
	}()
	return nil
}
