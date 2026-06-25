package probe

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"cacheproof/internal/config"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/tidwall/gjson"
)

func evaluateAssertions(assert config.Assertion, body []byte, baseline *Result, schemas map[string]*jsonschema.Schema, result *Result) {
	if assert.Status != 0 && result.Status != assert.Status {
		result.Failures = append(result.Failures, fmt.Sprintf("expected status %d, got %d", assert.Status, result.Status))
	}
	if assert.ExitCode != nil && result.ExitCode != *assert.ExitCode {
		result.Failures = append(result.Failures, fmt.Sprintf("expected exit code %d, got %d", *assert.ExitCode, result.ExitCode))
	}
	if assert.JSONSchema != "" {
		schema := schemas[assert.JSONSchema]
		if schema == nil {
			result.Failures = append(result.Failures, fmt.Sprintf("json schema %q was not compiled", assert.JSONSchema))
		} else {
			var decoded interface{}
			if err := json.Unmarshal(body, &decoded); err != nil {
				result.Failures = append(result.Failures, "json schema input is not valid JSON: "+err.Error())
			} else if err := schema.Validate(decoded); err != nil {
				result.Failures = append(result.Failures, "json schema validation failed: "+err.Error())
			}
		}
	}
	if baseline != nil {
		for _, path := range assert.JSONEqualsBaseline {
			if ok, message := compareGJSON(body, baseline.Body, path); !ok {
				result.Failures = append(result.Failures, message)
			}
		}
		if assert.MaxLatencyIncrease > 0 && baseline.Latency > 0 {
			allowed := baseline.Latency + time.Duration(float64(baseline.Latency)*assert.MaxLatencyIncrease)
			if result.Latency > allowed {
				result.Failures = append(result.Failures, fmt.Sprintf("latency increased above baseline by more than %.2f", assert.MaxLatencyIncrease))
			}
		}
	}
}

func compareGJSON(body []byte, baseline []byte, path string) (bool, string) {
	got := gjson.GetBytes(body, path)
	want := gjson.GetBytes(baseline, path)
	if !got.Exists() && !want.Exists() {
		return true, ""
	}
	if got.Exists() != want.Exists() {
		return false, fmt.Sprintf("field %s existence changed", path)
	}
	gotValue := got.Value()
	wantValue := want.Value()
	if reflect.DeepEqual(gotValue, wantValue) {
		return true, ""
	}
	return false, fmt.Sprintf("field %s changed vs baseline", path)
}
