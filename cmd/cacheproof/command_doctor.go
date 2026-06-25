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
	"cacheproof/internal/fault"
	"cacheproof/internal/probe"
	"cacheproof/internal/proxy"
	"cacheproof/internal/recorder"
	"cacheproof/internal/resp"

	"github.com/spf13/cobra"
)

type doctorOptions struct {
	configPath          string
	allowUnsafeCommands bool
	verbose             bool
}

func newDoctorCommand(ctx context.Context, stdout io.Writer, stderr io.Writer) *cobra.Command {
	opts := &doctorOptions{}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check config, upstream, proxy, and probes without fault injection",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(ctx, stdout, stderr, opts)
		},
	}
	cmd.Flags().StringVar(&opts.configPath, "config", "./cacheproof.yml", "config file path")
	cmd.Flags().BoolVar(&opts.allowUnsafeCommands, "allow-unsafe-commands", false, "allow shell command probes")
	cmd.Flags().BoolVarP(&opts.verbose, "verbose", "v", false, "verbose logs")
	return cmd
}

func runDoctor(ctx context.Context, stdout io.Writer, stderr io.Writer, opts *doctorOptions) error {
	cfg, err := config.Load(ctx, opts.configPath, config.LoadOptions{AllowUnsafeCommands: opts.allowUnsafeCommands})
	if err != nil {
		return err
	}
	runID, err := newRunID()
	if err != nil {
		return fmt.Errorf("%w: generate run_id: %v", appx.ErrInfrastructure, err)
	}
	logger := NewLogger(opts.verbose, stderr).With("run_id", runID)
	auth := doctorAuth(cfg)
	client, err := resp.DialContext(ctx, cfg.Proxy.Upstream, auth)
	if err != nil {
		return fmt.Errorf("%w: upstream unavailable: %v", appx.ErrInfrastructure, err)
	}
	if err := client.Close(); err != nil {
		logger.Debug("close doctor redis client", "error", err)
	}

	rec := recorder.New(cfg.MaxValueBytes)
	cacheProxy := proxy.New(cfg.Proxy.Listen, cfg.Proxy.Upstream, auth, rec, logger)
	cacheProxy.SetEngine(fault.PassThrough{})
	if err := cacheProxy.Start(ctx); err != nil {
		return err
	}
	defer func() {
		if err := cacheProxy.Shutdown(ctx); err != nil {
			logger.Error("shutdown doctor proxy", "error", err)
		}
	}()

	schemas, err := probe.CompileSchemas(ctx, cfg, filepath.Dir(opts.configPath))
	if err != nil {
		return fmt.Errorf("%w: compile schemas: %v", appx.ErrConfig, err)
	}
	runner := probe.NewRunner(&http.Client{}, opts.allowUnsafeCommands, schemas, logger)
	failed := false
	for _, cfgProbe := range cfg.Probes {
		result, err := runner.Run(ctx, cfgProbe, nil)
		if err != nil {
			return fmt.Errorf("%w: run doctor probe %q: %v", appx.ErrInfrastructure, cfgProbe.Name, err)
		}
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
			failed = true
		}
		if _, err := fmt.Fprintf(stdout, "%-15s %-4s   doctor probe\n", cfgProbe.Name, status); err != nil {
			return err
		}
	}
	if rec.Snapshot().ObservedCommandRows == 0 {
		failed = true
		if _, err := fmt.Fprintln(stdout, "no-redis-traffic FAIL   no Redis traffic observed through proxy; the app may connect directly to upstream"); err != nil {
			return err
		}
	}
	if failed {
		if _, err := fmt.Fprintln(stdout, "doctor          FAIL   checks failed"); err != nil {
			return err
		}
		return fmt.Errorf("%w: doctor checks failed", appx.ErrProbeFailed)
	}
	_, err = fmt.Fprintln(stdout, "doctor          PASS   checks completed")
	return err
}

func doctorAuth(cfg *config.Config) resp.Auth {
	auth := resp.Auth{}
	if cfg.Proxy.UpstreamUsernameEnv != "" {
		auth.Username = os.Getenv(cfg.Proxy.UpstreamUsernameEnv)
	}
	if cfg.Proxy.UpstreamPasswordEnv != "" {
		auth.Password = os.Getenv(cfg.Proxy.UpstreamPasswordEnv)
	}
	return auth
}
