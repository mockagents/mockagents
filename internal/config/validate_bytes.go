package config

import (
	"fmt"
	"strings"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// ValidateReport is the result of ValidateBytes — the detected
// document kind plus any accumulated validation errors. Callers
// check len(Errors) == 0 to decide whether the document is valid.
//
// Unlike the file-oriented loaders, ValidateBytes never returns a
// Go error for validation failures: the report carries all of them
// in Errors so web surfaces can render a single response shape for
// both parse-time and schema-time problems.
type ValidateReport struct {
	// Kind is the top-level `kind:` value from the document. Empty
	// when the document failed to parse at all.
	Kind string `json:"kind"`
	// Errors is the ordered list of validation problems. Line and
	// column are populated when the underlying yaml.Node tree
	// provided a location.
	Errors []*ValidationError `json:"errors"`
}

// ValidateBytes parses and validates a single YAML or JSON document
// handed in as raw bytes. It mirrors the behavior of LoadFile +
// Validator.Validate but operates on in-memory input — that lets the
// GUI editor and other tools exercise the same validation path
// without having to land the document on disk first.
//
// Supported kinds: Agent, Pipeline, TestSuite, MCPServer. Unknown
// kinds return a single "unknown kind" error so typos surface clearly.
// For kinds other than Agent the validator is parse-only (the typed
// decode catches structural problems); the Agent path additionally
// runs the full Validator.Validate rule set with line-number context.
func ValidateBytes(data []byte) *ValidateReport {
	report := &ValidateReport{}
	if len(strings.TrimSpace(string(data))) == 0 {
		report.Errors = append(report.Errors, &ValidationError{
			Field:   "document",
			Message: "document is empty",
		})
		return report
	}

	// Accept JSON as well: detect by leading character and convert
	// to YAML so yaml.Node line numbers still line up for later
	// reporting. Mirrors the readAndParse path in loader.go.
	if looksLikeJSON(data) {
		converted, err := jsonToYAML(data)
		if err != nil {
			report.Errors = append(report.Errors, &ValidationError{
				Field:   "document",
				Message: "invalid JSON: " + err.Error(),
			})
			return report
		}
		data = converted
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		report.Errors = append(report.Errors, parseErrorAsValidationError(err))
		return report
	}
	report.Kind = peekKind(&doc)

	switch report.Kind {
	case "", "Agent":
		// Empty kind defaults to Agent — matches LoadFile's
		// historical behavior where a missing kind was a
		// validator error rather than a parse failure.
		var def types.AgentDefinition
		if err := doc.Decode(&def); err != nil {
			report.Errors = append(report.Errors, parseErrorAsValidationError(err))
			return report
		}
		v := &Validator{}
		if errs := v.Validate(&def, "", &doc); errs != nil {
			report.Errors = append(report.Errors, errs.Errors...)
		}
		report.Kind = "Agent"
	case "Pipeline":
		var def types.PipelineDefinition
		if err := doc.Decode(&def); err != nil {
			report.Errors = append(report.Errors, parseErrorAsValidationError(err))
			return report
		}
		if errs := ValidatePipeline(&def, "", &doc); errs != nil {
			report.Errors = append(report.Errors, errs.Errors...)
		}
	case "TestSuite":
		var def types.TestSuiteDefinition
		if err := doc.Decode(&def); err != nil {
			report.Errors = append(report.Errors, parseErrorAsValidationError(err))
			return report
		}
		if errs := ValidateTestSuite(&def, "", &doc); errs != nil {
			report.Errors = append(report.Errors, errs.Errors...)
		}
	case "MCPServer":
		var def types.MCPServerDefinition
		if err := doc.Decode(&def); err != nil {
			report.Errors = append(report.Errors, parseErrorAsValidationError(err))
			return report
		}
		if errs := ValidateMCPServer(&def, "", &doc); errs != nil {
			report.Errors = append(report.Errors, errs.Errors...)
		}
	case "A2AServer":
		var def types.A2AServerDefinition
		if err := doc.Decode(&def); err != nil {
			report.Errors = append(report.Errors, parseErrorAsValidationError(err))
			return report
		}
		if errs := ValidateA2AServer(&def, "", &doc); errs != nil {
			report.Errors = append(report.Errors, errs.Errors...)
		}
	default:
		report.Errors = append(report.Errors, &ValidationError{
			Field:   "kind",
			Message: fmt.Sprintf("unknown kind %q (want Agent, Pipeline, TestSuite, MCPServer, or A2AServer)", report.Kind),
		})
	}
	return report
}

// looksLikeJSON is a cheap heuristic for bytes that open with
// JSON-style punctuation. yaml.Unmarshal actually accepts most JSON
// (it's a YAML subset) but jsonToYAML preserves key ordering and
// produces cleaner error messages on malformed JSON input.
func looksLikeJSON(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{', '[':
			return true
		default:
			return false
		}
	}
	return false
}

// parseErrorAsValidationError converts a yaml decode error into a
// ValidationError with best-effort line parsing. yaml.v3 error
// messages are of the form "yaml: line 12: some message"; we try to
// extract the line number so the GUI can highlight the right row.
func parseErrorAsValidationError(err error) *ValidationError {
	msg := err.Error()
	ve := &ValidationError{Field: "document", Message: msg}
	if idx := strings.Index(msg, "line "); idx >= 0 {
		rest := msg[idx+len("line "):]
		var ln int
		for i := 0; i < len(rest); i++ {
			if rest[i] < '0' || rest[i] > '9' {
				if i == 0 {
					break
				}
				fmt.Sscanf(rest[:i], "%d", &ln)
				break
			}
		}
		if ln > 0 {
			ve.Line = ln
		}
	}
	return ve
}
