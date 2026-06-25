package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"cacheproof/internal/appx"
	"cacheproof/internal/config"
	"cacheproof/internal/probe"
	"cacheproof/internal/report"
)

func (o *orchestrator) runMeasuredProbes(ctx context.Context, cfg *config.Config, baseline map[string]probe.Result) ([]report.ProbeInput, map[string]probe.Result, error) {
	outcomes := make([]report.ProbeInput, 0, len(cfg.Probes))
	results := make(map[string]probe.Result, len(cfg.Probes))
	for _, cfgProbe := range cfg.Probes {
		var base *probe.Result
		if baseline != nil {
			if existing, ok := baseline[cfgProbe.Name]; ok {
				copy := existing
				base = &copy
			}
		}
		result, err := o.runProbeWithTimeout(ctx, cfg.ProbeTimeout.Std(), cfgProbe, base)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: run probe %q: %v", appx.ErrInfrastructure, cfgProbe.Name, err)
		}
		results[cfgProbe.Name] = result
		outcomes = append(outcomes, report.ProbeInput{
			Name:     result.ProbeName,
			Passed:   result.Passed,
			Latency:  result.Latency,
			Failures: result.Failures,
		})
	}
	return outcomes, results, nil
}

func (o *orchestrator) runProbeWithTimeout(ctx context.Context, timeout time.Duration, cfgProbe config.Probe, baseline *probe.Result) (probe.Result, error) {
	probeCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		probeCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	return o.probeRunner.Run(probeCtx, cfgProbe, baseline)
}

func (o *orchestrator) runWarmup(ctx context.Context, cfg *config.Config) error {
	attempts := cfg.WarmupRetries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		var lastErr error
		for _, cfgProbe := range cfg.Probes {
			_, err := o.runProbeWithTimeout(ctx, cfg.ProbeTimeout.Std(), cfgProbe, nil)
			if err != nil {
				lastErr = err
				break
			}
		}
		if lastErr == nil {
			return nil
		}
		if attempt == attempts-1 {
			return lastErr
		}
		if err := waitContext(ctx, cfg.WarmupDelay.Std()); err != nil {
			return err
		}
	}
	return nil
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runResetCommand(ctx context.Context, command config.CommandSpec, timeout time.Duration) error {
	commandCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		commandCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	var cmd *exec.Cmd
	if len(command.Argv) > 0 {
		cmd = exec.CommandContext(commandCtx, command.Argv[0], command.Argv[1:]...)
	} else if command.Shell != "" {
		cmd = exec.CommandContext(commandCtx, "sh", "-c", command.Shell)
	} else {
		return fmt.Errorf("reset command has no argv or shell")
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if commandCtx.Err() != nil {
			return fmt.Errorf("reset command timed out: %w", commandCtx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("reset command exited with code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("run reset command: %w", err)
	}
	if commandCtx.Err() != nil {
		return fmt.Errorf("reset command timed out: %w", commandCtx.Err())
	}
	return nil
}
