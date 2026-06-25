package orchestrator

import (
	"context"
	"fmt"

	"cacheproof/internal/appx"
	"cacheproof/internal/config"
	"cacheproof/internal/fault"
	"cacheproof/internal/policy"
	"cacheproof/internal/probe"
	"cacheproof/internal/report"
	"cacheproof/internal/resp"
)

func (o *orchestrator) runScenario(
	ctx context.Context,
	cfg *config.Config,
	scenario config.Scenario,
	auth resp.Auth,
	cacheProxy ManagedProxy,
	baseline map[string]probe.Result,
	request RunRequest,
) (report.ScenarioInput, error) {
	cacheProxy.SetScenario(scenario.Name)
	if cfg.ResetCommand != nil {
		if err := runResetCommand(ctx, *cfg.ResetCommand, cfg.ProbeTimeout.Std()); err != nil {
			return report.ScenarioInput{}, fmt.Errorf("%w: reset before scenario %q: %v", appx.ErrInfrastructure, scenario.Name, err)
		}
	}
	if !request.NoWarmup {
		if err := o.runWarmup(ctx, cfg); err != nil {
			return report.ScenarioInput{}, fmt.Errorf("%w: warmup before scenario %q: %v", appx.ErrInfrastructure, scenario.Name, err)
		}
	}
	if err := pingUpstream(ctx, cfg.Proxy.Upstream, auth); err != nil {
		return report.ScenarioInput{}, fmt.Errorf("%w: upstream unavailable before scenario %q: %v", appx.ErrInfrastructure, scenario.Name, err)
	}
	if err := o.prepareScenario(ctx, cfg, scenario, auth, cacheProxy, request.AllowRemoteDestructive); err != nil {
		return report.ScenarioInput{}, err
	}
	probes, _, err := o.runMeasuredProbes(ctx, cfg, baseline)
	if err != nil {
		return report.ScenarioInput{}, err
	}
	return report.ScenarioInput{Name: scenario.Name, Probes: probes}, nil
}

func (o *orchestrator) prepareScenario(ctx context.Context, cfg *config.Config, scenario config.Scenario, auth resp.Auth, cacheProxy ManagedProxy, allowRemoteDestructive bool) error {
	switch scenario.Type {
	case "random_miss":
		cacheProxy.SetEngine(fault.RandomMiss{Seed: cfg.Seed, Probability: scenario.Probability})
	case "redis_unavailable":
		cacheProxy.SetEngine(fault.Unavailable{})
		cacheProxy.CloseAllConns()
	case "cold_cache":
		if _, err := policy.DeleteColdCache(ctx, cfg, cfg.Proxy.Upstream, auth, allowRemoteDestructive, o.logger); err != nil {
			return fmt.Errorf("%w: cold_cache: %v", appx.ErrInfrastructure, err)
		}
		cacheProxy.SetEngine(fault.PassThrough{})
	default:
		return fmt.Errorf("%w: unsupported scenario type %q", appx.ErrConfig, scenario.Type)
	}
	return nil
}
