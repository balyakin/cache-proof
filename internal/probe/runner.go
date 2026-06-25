package probe

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"

	"cacheproof/internal/config"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

type Runner interface {
	Run(ctx context.Context, probe config.Probe, baseline *Result) (Result, error)
}

type runner struct {
	httpClient         *http.Client
	allowUnsafeCommand bool
	schemas            map[string]*jsonschema.Schema
	logger             *slog.Logger
}

var _ Runner = (*runner)(nil)

func NewRunner(httpClient *http.Client, allowUnsafeCommand bool, schemas map[string]*jsonschema.Schema, logger *slog.Logger) Runner {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if schemas == nil {
		schemas = make(map[string]*jsonschema.Schema)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &runner{httpClient: httpClient, allowUnsafeCommand: allowUnsafeCommand, schemas: schemas, logger: logger}
}

func CompileSchemas(ctx context.Context, cfg *config.Config, configDir string) (map[string]*jsonschema.Schema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	schemas := make(map[string]*jsonschema.Schema)
	compiler := jsonschema.NewCompiler()
	for _, row := range cfg.Probes {
		path := row.Assert.JSONSchema
		if path == "" {
			continue
		}
		if _, ok := schemas[path]; ok {
			continue
		}
		abs := path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(configDir, path)
		}
		abs, err := filepath.Abs(abs)
		if err != nil {
			return nil, fmt.Errorf("resolve json schema %q: %w", path, err)
		}
		fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
		schema, err := compiler.Compile(fileURL)
		if err != nil {
			return nil, fmt.Errorf("compile json schema %q: %w", path, err)
		}
		schemas[path] = schema
	}
	return schemas, nil
}

func (r *runner) Run(ctx context.Context, cfgProbe config.Probe, baseline *Result) (Result, error) {
	result := Result{ProbeName: cfgProbe.Name, ExitCode: -1}
	var runErr error
	if cfgProbe.HTTP != nil {
		runErr = r.runHTTP(ctx, cfgProbe, &result)
	}
	if cfgProbe.Command != nil {
		if err := r.runCommand(ctx, cfgProbe, &result); runErr == nil {
			runErr = err
		}
	}
	if runErr != nil {
		result.Passed = false
		return result, runErr
	}
	evaluateAssertions(cfgProbe.Assert, result.Body, baseline, r.schemas, &result)
	expect := cfgProbe.Assert.Expect
	if expect == "" {
		expect = "pass"
	}
	if expect == "fail" {
		result.Passed = len(result.Failures) > 0
		if !result.Passed {
			result.Failures = append(result.Failures, "expected probe to fail, but assertions passed")
		}
		return result, nil
	}
	result.Passed = len(result.Failures) == 0
	return result, nil
}
