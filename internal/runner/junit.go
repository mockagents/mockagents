package runner

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// JUnit XML types. The schema below is the "Jenkins-compatible" subset
// understood by every mainstream CI test reporter (GitHub Actions,
// GitLab, CircleCI, Jenkins, Buildkite, JUnit-Jenkins plugin). We
// deliberately avoid the more fiddly properties/system-out elements
// — those are optional and not all parsers handle them uniformly.

// JUnitTestsuites is the top-level wrapper element.
type JUnitTestsuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr,omitempty"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Errors   int              `xml:"errors,attr"`
	Time     float64          `xml:"time,attr"`
	Suites   []JUnitTestsuite `xml:"testsuite"`
}

// JUnitTestsuite models a single MockAgents TestSuite.
type JUnitTestsuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Time     float64         `xml:"time,attr"`
	Cases    []JUnitTestcase `xml:"testcase"`
}

// JUnitTestcase models a single TestCase inside a suite.
type JUnitTestcase struct {
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
}

// JUnitFailure is emitted when a TestCase reports at least one
// assertion failure. Reporters render the Message attribute as the
// one-line summary and the chardata as the full detail.
type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

// toJUnit converts a slice of SuiteResult into the JUnit XML model.
// The top-level Testsuites time is the sum of suite latencies.
func toJUnit(results []*SuiteResult) *JUnitTestsuites {
	root := &JUnitTestsuites{Name: "mockagents"}
	for _, sr := range results {
		suite := JUnitTestsuite{
			Name:  sr.SuiteName,
			Tests: len(sr.Cases),
			Time:  sr.Latency.Seconds(),
		}
		for _, cr := range sr.Cases {
			tc := JUnitTestcase{
				Classname: sr.Target,
				Name:      cr.Name,
				Time:      cr.Latency.Seconds(),
			}
			if !cr.Passed {
				suite.Failures++
				tc.Failure = &JUnitFailure{
					Message: shortFailureMessage(cr),
					Type:    "AssertionFailure",
					Body:    strings.Join(cr.Failures, "\n"),
				}
				if cr.ErrMessage != "" {
					tc.Failure.Body = cr.ErrMessage + "\n" + tc.Failure.Body
					tc.Failure.Message = cr.ErrMessage
				}
			}
			suite.Cases = append(suite.Cases, tc)
		}
		root.Suites = append(root.Suites, suite)
		root.Tests += suite.Tests
		root.Failures += suite.Failures
		root.Time += suite.Time
	}
	return root
}

// WriteJUnit serializes the given suite results as JUnit XML.
// It writes the standard `<?xml version="1.0" encoding="UTF-8"?>`
// header because several CI parsers reject files without it.
func WriteJUnit(w io.Writer, results []*SuiteResult) error {
	root := toJUnit(results)
	if _, err := fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(root); err != nil {
		return err
	}
	return enc.Flush()
}

// shortFailureMessage picks the first failure line as the one-liner
// shown in CI report summaries. Empty string when the case passed or
// holds no failure text.
func shortFailureMessage(cr *CaseResult) string {
	if len(cr.Failures) == 0 {
		if cr.ErrMessage != "" {
			return cr.ErrMessage
		}
		return ""
	}
	return cr.Failures[0]
}
