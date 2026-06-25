package probe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"cacheproof/internal/config"
)

const maxHTTPResponseBytes = 64 * 1024 * 1024

func (r *runner) runHTTP(ctx context.Context, cfgProbe config.Probe, result *Result) error {
	spec := cfgProbe.HTTP
	request, err := http.NewRequestWithContext(ctx, spec.Method, spec.URL, bytes.NewBufferString(spec.Body))
	if err != nil {
		result.Failures = append(result.Failures, "http request build failed: "+err.Error())
		return nil
	}
	for key, value := range spec.Headers {
		request.Header.Set(key, value)
	}
	start := time.Now()
	response, err := r.httpClient.Do(request)
	result.Latency = time.Since(start)
	if err != nil {
		if ctx.Err() != nil {
			result.Failures = append(result.Failures, "probe timed out")
			return ctx.Err()
		}
		result.Failures = append(result.Failures, "http request failed: "+err.Error())
		return fmt.Errorf("http request failed: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			r.logger.Debug("close http response body", "error", closeErr)
		}
	}()
	result.Status = response.StatusCode
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHTTPResponseBytes+1))
	if err != nil {
		result.Failures = append(result.Failures, "http response read failed: "+err.Error())
		return fmt.Errorf("http response read failed: %w", err)
	}
	if len(body) > maxHTTPResponseBytes {
		result.Failures = append(result.Failures, "http response body exceeds maximum size")
		return nil
	}
	result.Body = body
	return nil
}
