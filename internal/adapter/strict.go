package adapter

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
)

// HeaderStrictViolation surfaces strict-tools violations detected in WARN
// mode (round-11): all checks ran, the request succeeded, and this header
// carries what strict mode would have rejected — assert on its absence to
// migrate a suite incrementally.
const HeaderStrictViolation = "X-Mockagents-Strict-Violation"

// setStrictViolationHeader advertises warn-mode strict-tools violations.
func setStrictViolationHeader(w http.ResponseWriter, resp *engine.Response) {
	if resp == nil || len(resp.StrictWarnings) == 0 {
		return
	}
	w.Header().Set(HeaderStrictViolation, strings.Join(resp.StrictWarnings, "; "))
}

// The renderers below translate an engine.StrictToolError into each
// provider's REAL 400 body (byte-exact where the round-11 eval captured
// verbatim texts — including OpenAI's own "preceeding" misspelling).
// Providers blame different sides of an id mismatch: OpenAI lists the
// unanswered call ids, Anthropic the unexpected result id, Gemini reports a
// structural parity failure; each renderer picks its provider's framing.

// writeOpenAIStrictError renders the Chat Completions 400.
func writeOpenAIStrictError(w http.ResponseWriter, se *engine.StrictToolError) {
	var msg string
	var param any
	switch se.Kind {
	case "orphan":
		msg = "Invalid parameter: messages with role 'tool' must be a response to a preceeding message with 'tool_calls'."
		param = fmt.Sprintf("messages.[%d].role", se.Index)
	case "unknown", "unanswered":
		ids := se.UnansweredIDs
		if len(ids) == 0 && se.UnknownID != "" {
			ids = []string{se.UnknownID}
		}
		msg = fmt.Sprintf("An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. The following tool_call_ids did not have response messages: %s", strings.Join(ids, ", "))
		param = "messages"
	case "unknown_tool":
		msg = fmt.Sprintf("Tool choice '%s' not found in 'tools' parameter.", se.UnknownID)
		param = "tool_choice"
	default:
		msg = se.Message
	}
	writeJSON(w, http.StatusBadRequest, openAIError{Error: openAIErrorBody{
		Type: "invalid_request_error", Message: msg, Param: param,
	}})
}

// writeResponsesStrictError renders the Responses API 400.
func writeResponsesStrictError(w http.ResponseWriter, se *engine.StrictToolError) {
	var msg string
	switch se.Kind {
	case "orphan", "unknown":
		id := se.UnknownID
		if id == "" {
			id = "(missing)"
		}
		msg = fmt.Sprintf("No tool call found for function call output with call_id %s.", id)
	case "unanswered":
		msg = fmt.Sprintf("No tool output found for function call %s.", strings.Join(se.UnansweredIDs, ", "))
	case "unknown_tool":
		// Verbatim capture from the round-11 eval (Responses surface).
		writeJSON(w, http.StatusBadRequest, openAIError{Error: openAIErrorBody{
			Type:    "invalid_request_error",
			Message: fmt.Sprintf("Tool choice '%s' not found in 'tools' parameter.", se.UnknownID),
			Param:   "tool_choice",
		}})
		return
	default:
		msg = se.Message
	}
	writeJSON(w, http.StatusBadRequest, openAIError{Error: openAIErrorBody{
		Type: "invalid_request_error", Message: msg, Param: nil,
	}})
}

// writeAnthropicStrictError renders the Messages API 400.
func writeAnthropicStrictError(w http.ResponseWriter, se *engine.StrictToolError) {
	var msg string
	switch se.Kind {
	case "orphan", "unknown":
		id := se.UnknownID
		if id == "" {
			id = "(missing)"
		}
		msg = fmt.Sprintf("messages.%d.content: unexpected `tool_use_id` found in `tool_result` blocks: %s. Each `tool_result` block must have a corresponding `tool_use` block in the previous message.", se.Index, id)
	case "unanswered":
		msg = fmt.Sprintf("`tool_use` ids were found without `tool_result` blocks immediately after: %s. Each `tool_use` block must have a corresponding `tool_result` block in the next message.", strings.Join(se.UnansweredIDs, ", "))
	case "unknown_tool":
		msg = fmt.Sprintf("tool_choice: tool name `%s` not found in `tools`.", se.UnknownID)
	default:
		msg = se.Message
	}
	writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", msg)
}

// writeGeminiStrictError renders the Gemini 400 — its real validation is
// structural (response parts must pair with call parts), so every id-family
// violation maps to the widely-observed parity message.
func writeGeminiStrictError(w http.ResponseWriter, se *engine.StrictToolError) {
	msg := se.Message
	switch {
	case se.Check == "ids":
		msg = "Please ensure that the number of function response parts is equal to the number of function call parts of the function call turn."
	case se.Kind == "unknown_tool":
		msg = fmt.Sprintf("Invalid value for allowed_function_names: function %s not declared in tools.", se.UnknownID)
	}
	writeGeminiError(w, http.StatusBadRequest, "INVALID_ARGUMENT", msg)
}
