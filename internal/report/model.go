package report

import (
	"time"

	"cacheproof/internal/policy"
	cacheproof "cacheproof/pkg/cacheproof"
)

type RunResult = cacheproof.RunResult

type ProbeInput struct {
	Name     string
	Passed   bool
	Latency  time.Duration
	Failures []string
}

type ScenarioInput struct {
	Name   string
	Probes []ProbeInput
}

func BuildRunResult(scenarios []ScenarioInput, findings []policy.Finding) RunResult {
	result := RunResult{
		Scenarios: make([]cacheproof.ScenarioOutcome, 0, len(scenarios)),
		Findings:  make([]cacheproof.Finding, 0, len(findings)),
	}
	for _, scenario := range scenarios {
		outcome := cacheproof.ScenarioOutcome{Name: scenario.Name, Passed: true}
		for _, probe := range scenario.Probes {
			if !probe.Passed {
				outcome.Passed = false
			}
			outcome.Probes = append(outcome.Probes, cacheproof.ProbeOutcome{
				Name:      probe.Name,
				Passed:    probe.Passed,
				LatencyMS: probe.Latency.Milliseconds(),
				Failures:  append([]string(nil), probe.Failures...),
			})
		}
		if outcome.Passed {
			result.Summary.Passed++
		} else {
			result.Summary.Failed++
		}
		result.Scenarios = append(result.Scenarios, outcome)
	}
	for _, finding := range findings {
		result.Findings = append(result.Findings, cacheproof.Finding{
			Name:    finding.Name,
			Level:   string(finding.Level),
			Message: finding.Message,
		})
		switch finding.Level {
		case policy.FAIL:
			result.Summary.Failed++
		case policy.WARN:
			result.Summary.Warnings++
		}
	}
	result.Disposable = result.Summary.Failed == 0
	return result
}

func ExitCodeForResult(result RunResult, failOn string) int {
	if result.Summary.Failed > 0 {
		return 1
	}
	if failOn == "warn" && result.Summary.Warnings > 0 {
		return 1
	}
	return 0
}
