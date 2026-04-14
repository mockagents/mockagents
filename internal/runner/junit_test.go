package runner

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestJUnitEmptyResultsRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, nil); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	// Must begin with XML declaration.
	if !strings.HasPrefix(buf.String(), `<?xml version="1.0"`) {
		t.Errorf("missing xml header: %q", buf.String()[:40])
	}
	// Must parse back into our model.
	var root JUnitTestsuites
	if err := xml.Unmarshal(buf.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if root.Tests != 0 || root.Failures != 0 {
		t.Errorf("expected 0/0, got tests=%d failures=%d", root.Tests, root.Failures)
	}
}

func TestJUnitPassingSuite(t *testing.T) {
	results := []*SuiteResult{{
		SuiteName: "support",
		Target:    "agent:support-agent",
		Passed:    1,
		Failed:    0,
		Latency:   250 * time.Millisecond,
		Cases: []*CaseResult{{
			Name:    "order-happy-path",
			Passed:  true,
			Latency: 50 * time.Millisecond,
		}},
	}}
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, results); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	body := buf.String()
	// Attribute spot-checks keep the test tight without pinning the
	// exact whitespace layout produced by encoding/xml.
	for _, expect := range []string{
		`tests="1"`,
		`failures="0"`,
		`name="support"`,
		`classname="agent:support-agent"`,
		`name="order-happy-path"`,
	} {
		if !strings.Contains(body, expect) {
			t.Errorf("missing substring %q in:\n%s", expect, body)
		}
	}
	if strings.Contains(body, "<failure") {
		t.Errorf("passing suite should not emit <failure>: %s", body)
	}
}

func TestJUnitFailingCaseEmitsFailureElement(t *testing.T) {
	results := []*SuiteResult{{
		SuiteName: "support",
		Target:    "agent:support-agent",
		Passed:    0,
		Failed:    1,
		Latency:   40 * time.Millisecond,
		Cases: []*CaseResult{{
			Name:    "order-missing-tool",
			Passed:  false,
			Latency: 40 * time.Millisecond,
			Failures: []string{
				`tool_call: expected call to "lookup_order" with args map[id:ORD-1], got []`,
				`response_contains: "shipped" not found in "hello"`,
			},
		}},
	}}
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, results); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, `<failure`) {
		t.Fatalf("expected <failure> element, got:\n%s", body)
	}
	// The first failure string becomes the message attribute.
	if !strings.Contains(body, `tool_call: expected call to`) {
		t.Errorf("message attr missing first failure: %s", body)
	}
	// The chardata carries the joined body with both entries.
	if !strings.Contains(body, `response_contains`) {
		t.Errorf("chardata missing second failure: %s", body)
	}
	// Top-level aggregate counts roll up.
	if !strings.Contains(body, `failures="1"`) {
		t.Errorf("top-level failures count wrong: %s", body)
	}
}

func TestJUnitEngineErrorSurfacedInMessage(t *testing.T) {
	results := []*SuiteResult{{
		SuiteName: "broken",
		Target:    "agent:ghost",
		Passed:    0,
		Failed:    1,
		Latency:   1 * time.Millisecond,
		Cases: []*CaseResult{{
			Name:       "no-such-agent",
			Passed:     false,
			Failures:   []string{`engine error: agent "ghost" not found`},
			ErrMessage: `agent "ghost" not found`,
		}},
	}}
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, results); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	// The ErrMessage path should surface as the failure message.
	if !strings.Contains(buf.String(), `message="agent &#34;ghost&#34; not found"`) &&
		!strings.Contains(buf.String(), `message="agent &quot;ghost&quot; not found"`) {
		t.Errorf("engine error not reflected in message attr: %s", buf.String())
	}
}
