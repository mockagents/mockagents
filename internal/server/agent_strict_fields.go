package server

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
)

// UX-03: strict unknown-field rejection, opted into by conditional writes.
//
// The typed YAML decode silently discards any field it does not recognise, so
// a client could PUT a document containing a field the server does not support
// and get back 201 / "persisted": true while that field was quietly dropped on
// the way to disk. For a hand-written curl that is merely surprising. For an
// editor doing GET → modify → PUT it is data loss: every round trip strips
// whatever the GUI's types do not model.
//
// Epic §8.2 requires rejecting unsupported fields until a lossless round-trip
// is proven. Doing that unconditionally would break existing clients that
// happen to send extra keys, which is exactly the compatibility promise the
// additive conditional-write route was approved to protect.
//
// So strict checking rides along with the opt-in: a request that sends
// If-Match or If-None-Match has adopted the new contract and gets unknown
// fields rejected; an unconditional PUT or POST keeps the old lenient
// behaviour. One opt-in, one coherent set of guarantees.

// hasWritePrecondition reports whether the caller opted into the conditional
// write contract, and therefore into strict field checking.
func hasWritePrecondition(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("If-Match")) != "" ||
		strings.TrimSpace(r.Header.Get("If-None-Match")) != ""
}

// yamlUnknownFieldRe matches yaml.v3's TypeError text for an unknown key, e.g.
//
//	line 8: field someFutureField not found in type types.AgentSpec
var yamlUnknownFieldRe = regexp.MustCompile(`^line (\d+): field (\S+) not found in type (\S+)$`)

// decodeAgentStrict parses body with unknown-field rejection enabled. It
// returns validation errors describing each unsupported field rather than a
// raw Go type error, so a UI can point at the offending line.
func decodeAgentStrict(body []byte) (*types.AgentDefinition, []*config.ValidationError) {
	var def types.AgentDefinition
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)

	err := dec.Decode(&def)
	if err == nil {
		return &def, nil
	}

	var typeErr *yaml.TypeError
	if !isTypeError(err, &typeErr) {
		// A syntax error, not an unknown field. Let the lenient path report it
		// in its usual shape so the message stays consistent.
		return nil, nil
	}

	errs := make([]*config.ValidationError, 0, len(typeErr.Errors))
	for _, msg := range typeErr.Errors {
		errs = append(errs, translateStrictFieldError(msg))
	}
	return nil, errs
}

// isTypeError unwraps err into a *yaml.TypeError when it is one.
func isTypeError(err error, out **yaml.TypeError) bool {
	te, ok := err.(*yaml.TypeError)
	if ok {
		*out = te
	}
	return ok
}

// translateStrictFieldError turns one yaml.v3 TypeError line into a
// ValidationError that names the field instead of a Go type.
func translateStrictFieldError(msg string) *config.ValidationError {
	m := yamlUnknownFieldRe.FindStringSubmatch(strings.TrimSpace(msg))
	if m == nil {
		// Unrecognised shape: surface it verbatim rather than inventing a
		// tidier message that might misdescribe the problem.
		return &config.ValidationError{Field: "(document)", Message: msg}
	}
	line, _ := strconv.Atoi(m[1])
	field := m[2]
	return &config.ValidationError{
		Line:    line,
		Field:   field,
		Message: fmt.Sprintf("unsupported field %q: this server does not recognise it", field),
		Suggestion: "Remove the field, or check it against the agent schema. It is rejected rather " +
			"than ignored because a conditional write was requested: silently dropping it would " +
			"delete it from the stored definition.",
	}
}

// checkStrictFields runs the strict decode when the caller opted in, and writes
// a 422 listing the unsupported fields. It returns ok=false when it has already
// responded.
func checkStrictFields(w http.ResponseWriter, r *http.Request, body []byte) bool {
	if !hasWritePrecondition(r) {
		return true
	}
	if _, errs := decodeAgentStrict(body); len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, ValidateResponse{
			OK: false, Kind: string(types.AgentKind), Errors: errs,
		})
		return false
	}
	return true
}
