package report

import (
	"encoding/xml"
	"io"
)

type junitTestsuite struct {
	XMLName  xml.Name        `xml:"testsuite"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Cases    []junitTestcase `xml:"testcase"`
}

type junitTestcase struct {
	Name    string        `xml:"name,attr"`
	Time    string        `xml:"time,attr,omitempty"`
	Failure *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func WriteJUnit(w io.Writer, result RunResult, failOn string) error {
	suite := junitTestsuite{Name: "cacheproof"}
	for _, scenario := range result.Scenarios {
		for _, probe := range scenario.Probes {
			tc := junitTestcase{Name: scenario.Name + "/" + probe.Name}
			if !probe.Passed {
				message := firstFailure(probe.Failures)
				tc.Failure = &junitFailure{Message: message, Text: message}
				suite.Failures++
			}
			suite.Cases = append(suite.Cases, tc)
		}
	}
	for _, finding := range result.Findings {
		failed := finding.Level == "FAIL" || (finding.Level == "WARN" && failOn == "warn")
		if !failed {
			continue
		}
		tc := junitTestcase{Name: "finding/" + finding.Name}
		tc.Failure = &junitFailure{Message: finding.Message, Text: finding.Message}
		suite.Failures++
		suite.Cases = append(suite.Cases, tc)
	}
	suite.Tests = len(suite.Cases)
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	if err := encoder.Encode(suite); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
