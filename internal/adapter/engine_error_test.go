package adapter

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
)

// TestEngineErrorStatus is the audit M-19 guard. Classification used to read
// the error's prose: any message containing "not found" became a 404 and any
// message containing "empty" became a 400, so a failure inside a scenario or
// agent whose NAME happened to contain one of those words was reported to the
// caller as their mistake.
func TestEngineErrorStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"agent not found", engine.ErrAgentNotFound, http.StatusNotFound},
		{"agent not found, wrapped", fmt.Errorf("resolving agent: %w", engine.ErrAgentNotFound), http.StatusNotFound},
		{"empty message", engine.ErrEmptyMessage, http.StatusBadRequest},
		{"empty message, wrapped", fmt.Errorf("validating: %w", engine.ErrEmptyMessage), http.StatusBadRequest},
		{"unrelated failure", errors.New("template execution failed"), http.StatusInternalServerError},
		{"nil", nil, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engineErrorStatus(tc.err); got != tc.want {
				t.Errorf("engineErrorStatus(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestEngineErrorStatusIgnoresProse pins the actual defect: an error whose
// text contains the old trigger words, but which is not one of the typed
// conditions, is a server error — not a 404 or a 400 blaming the caller.
func TestEngineErrorStatusIgnoresProse(t *testing.T) {
	misleading := []error{
		errors.New(`rendering scenario "empty-cart": template: bad function`),
		errors.New(`scenario "order not found" failed to render`),
		errors.New(`agent "cart-empty-state" generator panicked`),
	}
	for _, err := range misleading {
		if got := engineErrorStatus(err); got != http.StatusInternalServerError {
			t.Errorf("engineErrorStatus(%q) = %d, want 500 — it was classified by its wording", err, got)
		}
	}
}

func TestEngineErrorIsNotFound(t *testing.T) {
	if !engineErrorIsNotFound(fmt.Errorf("wrapped: %w", engine.ErrAgentNotFound)) {
		t.Error("a wrapped ErrAgentNotFound should be recognized")
	}
	if engineErrorIsNotFound(errors.New("scenario not found in cassette")) {
		t.Error("an unrelated error mentioning 'not found' must not be treated as the engine condition")
	}
}
