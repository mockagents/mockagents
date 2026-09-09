package adapter

import (
	"errors"
	"net/http"

	"github.com/mockagents/mockagents/internal/engine"
)

// engineErrorStatus maps an engine failure to the HTTP status every provider
// surface should report for it.
//
// Every adapter used to classify these by substring — `strings.Contains(err
// .Error(), "not found")` for 404 and `"empty"` for 400 — which reads the
// error's prose as if it were an API (audit M-19). An agent or scenario whose
// name merely contained one of those words was misclassified: a template
// failure inside a scenario called `empty-cart` came back as a 400 telling the
// caller their request was invalid. Six copies of that logic also meant six
// places to drift.
//
// The mapping is by typed sentinel via errors.Is, so it survives wrapping and
// says out loud which engine conditions have a wire meaning. Anything else is
// a 500: an unrecognized failure is the server's problem, not the caller's,
// and guessing otherwise is what this replaces.
func engineErrorStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, engine.ErrAgentNotFound):
		return http.StatusNotFound
	case errors.Is(err, engine.ErrEmptyMessage):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// engineErrorIsNotFound reports whether err is the engine's "no such agent"
// condition. Adapters whose error envelope names a type per status (Bedrock)
// use this rather than comparing the status back to 404.
func engineErrorIsNotFound(err error) bool {
	return errors.Is(err, engine.ErrAgentNotFound)
}
