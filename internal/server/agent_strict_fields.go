package server

import (
	"net/http"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
)

// Unknown-field rejection on the agent write API.
//
// The typed YAML decode silently discards any field it does not recognise, so
// a client could PUT a document containing a misspelled or unsupported field
// and get back 201 / "persisted": true while that field was quietly dropped on
// the way to disk. For an editor doing GET → modify → PUT that is data loss,
// and for a hand-written document it means the author's intent (a misspelled
// `streaming:` or `chaos:` block) silently never happens.
//
// This used to apply only to callers that sent If-Match / If-None-Match (the
// UX-03 opt-in). Since the 2026-10-06 quality review unknown fields are a
// validation error on every authoring surface — the CLI, the loader, the GUI
// validator and MCP management — so the write API rejects them
// unconditionally too, matching the JSON schema's additionalProperties:false.

// checkStrictFields writes a 422 listing the unsupported fields in body and
// returns ok=false when there are any.
func checkStrictFields(w http.ResponseWriter, body []byte) bool {
	if errs := config.UnknownAgentFields(body); len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, ValidateResponse{
			OK: false, Kind: string(types.AgentKind), Errors: errs,
		})
		return false
	}
	return true
}
