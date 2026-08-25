package drift

import (
	"encoding/xml"
	"fmt"
)

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Errors   int         `xml:"errors,attr"`
	Cases    []junitCase `xml:"testcase"`
}

// JUnit renders findings as deterministic test cases. Critical drift is a
// failure; warning and informational findings remain visible without failing
// the suite. A compatible report emits one passing compatibility test.
func JUnit(report Report) ([]byte, error) {
	suite := junitSuite{Name: "MockAgents provider drift: " + report.Operation, Errors: 0}
	if len(report.Findings) == 0 {
		suite.Cases = []junitCase{{Name: "compatible", Classname: report.Operation}}
	} else {
		suite.Cases = make([]junitCase, 0, len(report.Findings))
		for _, finding := range report.Findings {
			message := FindingDetail(finding)
			item := junitCase{Name: finding.Rule + " " + finding.Path, Classname: finding.Operation}
			if finding.Severity == SeverityCritical {
				item.Failure = &junitFailure{Message: message, Type: finding.Rule, Body: junitDetails(report, finding)}
				suite.Failures++
			} else {
				item.SystemOut = junitDetails(report, finding)
			}
			suite.Cases = append(suite.Cases, item)
		}
	}
	suite.Tests = len(suite.Cases)
	out, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(out, '\n')...), nil
}

func junitDetails(report Report, finding Finding) string {
	details := fmt.Sprintf("severity=%s rule=%s path=%s", finding.Severity, finding.Rule, finding.Path)
	if len(finding.Values) != 0 {
		details += fmt.Sprintf(" values=%q", finding.Values)
	}
	if report.Adapter != "" {
		details += " adapter=" + report.Adapter
	}
	return details
}
