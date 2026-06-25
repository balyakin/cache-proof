package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"cacheproof/internal/appx"
	"cacheproof/internal/config"
	"cacheproof/internal/orchestrator"
	"cacheproof/internal/probe"
	"cacheproof/internal/report"

	"github.com/spf13/cobra"
)

type testOptions struct {
	configPath             string
	reportJSON             string
	reportJUnit            string
	failOn                 string
	only                   string
	noWarmup               bool
	allowUnsafeCommands    bool
	allowRemoteDestructive bool
	verbose                bool
}

func newTestCommand(ctx context.Context, stdout io.Writer, stderr io.Writer) *cobra.Command {
	opts := &testOptions{}
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run cache disposability scenarios",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTest(ctx, stdout, stderr, opts)
		},
	}
	cmd.Flags().StringVar(&opts.configPath, "config", "./cacheproof.yml", "config file path")
	cmd.Flags().StringVar(&opts.reportJSON, "report-json", "", "write JSON report to path")
	cmd.Flags().StringVar(&opts.reportJUnit, "report-junit", "", "write JUnit XML report to path")
	cmd.Flags().StringVar(&opts.failOn, "fail-on", "fail", "fail or warn")
	cmd.Flags().StringVar(&opts.only, "only", "", "run only one configured fault scenario after baseline")
	cmd.Flags().BoolVar(&opts.noWarmup, "no-warmup", false, "disable warm-up probes")
	cmd.Flags().BoolVar(&opts.allowUnsafeCommands, "allow-unsafe-commands", false, "allow shell command probes")
	cmd.Flags().BoolVar(&opts.allowRemoteDestructive, "allow-remote-destructive", false, "allow destructive cold_cache on non-loopback upstream")
	cmd.Flags().BoolVarP(&opts.verbose, "verbose", "v", false, "verbose logs")
	return cmd
}

func runTest(ctx context.Context, stdout io.Writer, stderr io.Writer, opts *testOptions) error {
	if opts.failOn != "fail" && opts.failOn != "warn" {
		return fmt.Errorf("%w: --fail-on must be fail or warn", appx.ErrUsage)
	}
	cfg, err := config.Load(ctx, opts.configPath, config.LoadOptions{AllowUnsafeCommands: opts.allowUnsafeCommands})
	if err != nil {
		return err
	}
	runID, err := newRunID()
	if err != nil {
		return fmt.Errorf("%w: generate run_id: %v", appx.ErrInfrastructure, err)
	}
	logger := NewLogger(opts.verbose, stderr).With("run_id", runID)
	schemas, err := probe.CompileSchemas(ctx, cfg, filepath.Dir(opts.configPath))
	if err != nil {
		return fmt.Errorf("%w: compile schemas: %v", appx.ErrConfig, err)
	}
	probeRunner := probe.NewRunner(&http.Client{}, opts.allowUnsafeCommands, schemas, logger)
	orch := orchestrator.New(nil, probeRunner, logger)
	result, err := orch.Run(ctx, orchestrator.RunRequest{
		Config:                 cfg,
		ConfigPath:             opts.configPath,
		OnlyScenario:           opts.only,
		NoWarmup:               opts.noWarmup,
		FailOn:                 opts.failOn,
		AllowRemoteDestructive: opts.allowRemoteDestructive,
	})
	if err != nil {
		return err
	}
	if err := report.WriteTerminal(stdout, result); err != nil {
		return fmt.Errorf("%w: write terminal report: %v", appx.ErrInfrastructure, err)
	}
	if opts.reportJSON != "" {
		if err := writeJSONReport(opts.reportJSON, result); err != nil {
			return err
		}
	}
	if opts.reportJUnit != "" {
		if err := writeJUnitReport(opts.reportJUnit, result, opts.failOn); err != nil {
			return err
		}
	}
	if report.ExitCodeForResult(result, opts.failOn) != 0 {
		return fmt.Errorf("%w: cache disposability failed", appx.ErrProbeFailed)
	}
	return nil
}

func writeJSONReport(path string, result report.RunResult) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("%w: create json report %q: %v", appx.ErrInfrastructure, path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("%w: close json report %q: %v", appx.ErrInfrastructure, path, closeErr)
		}
	}()
	if err := report.WriteJSON(file, result); err != nil {
		return fmt.Errorf("%w: write json report %q: %v", appx.ErrInfrastructure, path, err)
	}
	return nil
}

func writeJUnitReport(path string, result report.RunResult, failOn string) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("%w: create junit report %q: %v", appx.ErrInfrastructure, path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("%w: close junit report %q: %v", appx.ErrInfrastructure, path, closeErr)
		}
	}()
	if err := report.WriteJUnit(file, result, failOn); err != nil {
		return fmt.Errorf("%w: write junit report %q: %v", appx.ErrInfrastructure, path, err)
	}
	return nil
}
