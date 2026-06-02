package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/mockagents/mockagents/internal/config"
)

// maxValidateBodyBytes caps the YAML/JSON a caller may submit to the
// validate endpoint (X-DOS-001). Agent definitions are small; 1 MiB is
// generous and stops an unbounded ReadAll / YAML alias-bomb from OOMing
// the process.
const maxValidateBodyBytes = 1 << 20

// ValidateHandler serves POST /api/v1/config/validate. It accepts a
// raw YAML (or JSON) agent definition body, runs it through the
// same validator the CLI uses, and returns a structured report the
// GUI's in-browser editor renders inline.
//
// The handler is stateless — it can be mounted unconditionally.
type ValidateHandler struct{}

// NewValidateHandler returns a ready-to-mount ValidateHandler.
func NewValidateHandler() *ValidateHandler {
	return &ValidateHandler{}
}

// validateResponse is the JSON shape returned to the client.
// `ok` is a convenience boolean so the GUI doesn't need to inspect
// the error array length.
type validateResponse struct {
	OK     bool                      `json:"ok"`
	Kind   string                    `json:"kind"`
	Errors []*config.ValidationError `json:"errors"`
}

// ServeHTTP implements http.Handler.
func (h *ValidateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxValidateBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reading body: " + err.Error()})
		return
	}

	// Support both framings: raw YAML body (the default for the GUI
	// editor) and a JSON wrapper `{"yaml": "..."}` so curl scripts
	// can avoid shell-escaping multi-line YAML. We detect JSON by
	// content type; falling through to raw-YAML on unmarshal errors
	// keeps the interface forgiving.
	payload := body
	if isJSONContent(r.Header.Get("Content-Type"), body) {
		var wrapper struct {
			YAML string `json:"yaml"`
		}
		if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.YAML != "" {
			payload = []byte(wrapper.YAML)
		}
	}

	report := config.ValidateBytes(payload)
	writeJSON(w, http.StatusOK, validateResponse{
		OK:     len(report.Errors) == 0,
		Kind:   report.Kind,
		Errors: report.Errors,
	})
}

// isJSONContent returns true when either the Content-Type header
// announces JSON or the body starts with a JSON-object opening
// brace. YAML never legally starts with `{` so the bytewise check
// is a safe fallback when callers forget the header.
func isJSONContent(contentType string, body []byte) bool {
	if strings.HasPrefix(contentType, "application/json") {
		return true
	}
	for _, b := range body {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}
