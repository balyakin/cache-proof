package probe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"cacheproof/internal/config"
)

const maxCommandOutput = 64 * 1024

func (r *runner) runCommand(ctx context.Context, cfgProbe config.Probe, result *Result) error {
	spec := cfgProbe.Command
	var cmd *exec.Cmd
	if len(spec.Argv) > 0 {
		cmd = exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	} else if spec.Shell != "" {
		if !r.allowUnsafeCommand {
			result.Failures = append(result.Failures, "shell command requires --allow-unsafe-commands")
			return nil
		}
		cmd = exec.CommandContext(ctx, "sh", "-c", spec.Shell)
	} else {
		result.Failures = append(result.Failures, "command probe has no argv or shell")
		return nil
	}

	var output limitedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	start := time.Now()
	err := cmd.Run()
	result.Latency = time.Since(start)
	result.Output = output.Bytes()
	if ctx.Err() != nil {
		result.Failures = append(result.Failures, "probe timed out")
		return ctx.Err()
	}
	if err == nil {
		result.ExitCode = 0
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return nil
	}
	result.Failures = append(result.Failures, "command failed: "+err.Error())
	return fmt.Errorf("command failed: %w", err)
}

type limitedBuffer struct {
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := maxCommandOutput - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buf.Write(p[:remaining])
		} else {
			_, _ = b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}
