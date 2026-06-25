package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"cacheproof/internal/appx"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCommand(ctx, os.Stdout, os.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(appx.ExitCode(err))
	}
}

func newRootCommand(ctx context.Context, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "cacheproof",
		Short:         "Redis cache disposability checker",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newInitCommand(stdout))
	cmd.AddCommand(newTestCommand(ctx, stdout, stderr))
	cmd.AddCommand(newDoctorCommand(ctx, stdout, stderr))
	cmd.AddCommand(newVersionCommand(stdout))
	return cmd
}

func NewLogger(verbose bool, output io.Writer) *slog.Logger {
	// Allowed fields: run_id, scenario, conn_id, cmd, key_hash, latency_ms, pattern, count, error.
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	var handler slog.Handler
	handler = slog.NewTextHandler(output, &slog.HandlerOptions{Level: level})
	if verbose {
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})
	}
	return slog.New(handler)
}

func newRunID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
