package report

import (
	"fmt"
	"io"
)

func WriteTerminal(w io.Writer, result RunResult) error {
	for _, scenario := range result.Scenarios {
		status := "PASS"
		message := fmt.Sprintf("%d probes passed", len(scenario.Probes))
		if !scenario.Passed {
			status = "FAIL"
			message = "probe failed"
			for _, probe := range scenario.Probes {
				if !probe.Passed {
					message = probe.Name + ": " + firstFailure(probe.Failures)
					break
				}
			}
		}
		if _, err := fmt.Fprintf(w, "%-15s %-4s   %s\n", scenario.Name, status, message); err != nil {
			return err
		}
	}
	for _, finding := range result.Findings {
		if _, err := fmt.Fprintf(w, "%-15s %-4s   %s\n", finding.Name, finding.Level, finding.Message); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	status := "PASSED"
	if !result.Disposable {
		status = "FAILED"
	}
	_, err := fmt.Fprintf(w, "Cache disposability: %s  (%d failed, %d warning, %d passed)\n",
		status, result.Summary.Failed, result.Summary.Warnings, result.Summary.Passed)
	return err
}

func firstFailure(failures []string) string {
	if len(failures) == 0 {
		return "failed"
	}
	return failures[0]
}
