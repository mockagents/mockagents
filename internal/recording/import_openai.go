package recording

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ImportOpenAIStored parses an OpenAI "stored completions" JSONL export (one
// JSON object per line) into MockAgents interactions targeting
// POST /v1/chat/completions.
//
// The accepted line shapes are a MockAgents-defined contract (OpenAI's raw
// export schema is not stable); a line that does not match either is skipped
// with a reason rather than failing the whole import:
//
//	Shape A (envelope, preferred):
//	  {"request": {<chat-completions request>}, "response": {<chat.completion>}}
//
//	Shape B (flat stored ChatCompletion): a ChatCompletion object that also
//	carries its input — the request is reconstructed from "model" plus "input"
//	or "messages", with any temperature/top_p/max_tokens copied across, and the
//	object itself is the response.
func ImportOpenAIStored(r io.Reader) ([]*Interaction, ImportResult, error) {
	var out []*Interaction
	var res ImportResult

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), MaxCassetteLine)
	line := 0
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			res.skip(line, "invalid JSON")
			continue
		}

		reqBody, respBody, path, reason := reconstructStoredCompletion(fields)
		if reason != "" {
			res.skip(line, reason)
			continue
		}
		out = append(out, &Interaction{
			Method:          "POST",
			Path:            path,
			RequestBody:     reqBody,
			ResponseStatus:  200,
			ResponseHeaders: map[string]string{"Content-Type": "application/json"},
			ResponseBody:    respBody,
		})
		res.Imported++
	}
	if err := sc.Err(); err != nil {
		return nil, ImportResult{}, fmt.Errorf("reading stored-completions file: %w", err)
	}
	return out, res, nil
}

// reconstructStoredCompletion derives the request + response bodies and the
// target path from one JSONL line. Returns a non-empty reason when the line
// can't be imported. A line carrying "messages" is a Chat Completions request
// (→ /v1/chat/completions); one carrying only "input" is a Responses request
// (→ /v1/responses) — keeping the original input key + path so the imported
// interaction hash-matches the originating client.
func reconstructStoredCompletion(fields map[string]json.RawMessage) (req, resp json.RawMessage, path, reason string) {
	// Shape A: explicit request/response envelope.
	if rq, ok := fields["request"]; ok {
		rs, ok := fields["response"]
		if !ok {
			return nil, nil, "", "envelope has 'request' but no 'response'"
		}
		if !json.Valid(rq) || !json.Valid(rs) {
			return nil, nil, "", "envelope request/response is not valid JSON"
		}
		return rq, rs, pathForRequest(rq), ""
	}

	// Shape B: flat stored completion (has choices = the output).
	if _, ok := fields["choices"]; !ok {
		return nil, nil, "", "unrecognized shape (no 'request' or 'choices' key)"
	}
	inputKey, p := "messages", "/v1/chat/completions"
	if _, ok := fields["messages"]; !ok {
		if _, ok := fields["input"]; ok {
			inputKey, p = "input", "/v1/responses"
		} else {
			return nil, nil, "", "flat shape missing input ('input' or 'messages') — cannot reconstruct request"
		}
	}

	recon := map[string]json.RawMessage{inputKey: fields[inputKey]}
	if model, ok := fields["model"]; ok {
		recon["model"] = model
	}
	for _, sp := range []string{"temperature", "top_p", "max_tokens", "max_completion_tokens", "max_output_tokens", "tools", "tool_choice", "response_format"} {
		if v, ok := fields[sp]; ok {
			recon[sp] = v
		}
	}
	reqBody, err := json.Marshal(recon)
	if err != nil {
		return nil, nil, "", "failed to reconstruct request body"
	}
	// The full flat object is the response.
	respBody, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, "", "failed to re-encode response body"
	}
	return reqBody, respBody, p, ""
}

// pathForRequest picks the replay path for an envelope request: a request with
// "input" but no "messages" is a Responses API call, everything else is Chat
// Completions.
func pathForRequest(rq json.RawMessage) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(rq, &m) == nil {
		_, hasMsg := m["messages"]
		_, hasInput := m["input"]
		if hasInput && !hasMsg {
			return "/v1/responses"
		}
	}
	return "/v1/chat/completions"
}

// skip appends a per-line skip reason and bumps the counter.
func (res *ImportResult) skip(line int, reason string) {
	res.Skipped++
	res.SkipReasons = append(res.SkipReasons, fmt.Sprintf("line %d: %s — skipping", line, reason))
}
