package recording

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
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

	br := bufio.NewReaderSize(r, 64*1024)
	line := 0
	for {
		raw, tooLong, err := readCappedLine(br, MaxCassetteLine)
		atEOF := errors.Is(err, io.EOF)
		if err != nil && !atEOF {
			return nil, ImportResult{}, fmt.Errorf("reading stored-completions file: %w", err)
		}

		// A trailing newline yields one final empty read that is not a line.
		if !atEOF || tooLong || len(raw) > 0 {
			line++
			raw = bytes.TrimSpace(raw)
			switch {
			case tooLong:
				// One conversation over the cap must not cost the user the
				// other thousand lines in the export.
				res.skip(line, fmt.Sprintf("line is larger than the %d MiB limit", MaxCassetteLine>>20))
			case len(raw) == 0:
				// Blank separator line; counted so reported line numbers match
				// the file.
			default:
				importStoredLine(raw, line, &out, &res)
			}
		}
		if atEOF {
			break
		}
	}
	return out, res, nil
}

// importStoredLine turns one non-empty JSONL line into an interaction, or
// records why it could not be.
func importStoredLine(raw []byte, line int, out *[]*Interaction, res *ImportResult) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		res.skip(line, "invalid JSON")
		return
	}

	reqBody, respBody, path, reason := reconstructStoredCompletion(fields)
	if reason != "" {
		res.skip(line, reason)
		return
	}
	*out = append(*out, &Interaction{
		Method:          "POST",
		Path:            path,
		RequestBody:     reqBody,
		ResponseStatus:  200,
		ResponseHeaders: map[string]string{"Content-Type": "application/json"},
		ResponseBody:    respBody,
	})
	res.Imported++
}

// readCappedLine reads one newline-terminated line of at most max bytes.
//
// bufio.Scanner cannot be used here: it stops for good on bufio.ErrTooLong, so
// a single oversized line ends the import and every valid line after it is
// lost. This drains an oversized line instead and reports it via tooLong, so
// the caller can skip that one line and keep going.
//
// The returned error is io.EOF on the last line (which may still carry data
// when the file does not end with a newline), or a read error.
func readCappedLine(br *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	for {
		chunk, err := br.ReadSlice('\n')
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			if tooLong {
				continue // still draining the oversized line
			}
			line = append(line, chunk...)
			if len(line) > max {
				tooLong, line = true, nil
			}
		case err != nil:
			if tooLong {
				return nil, true, err
			}
			line = append(line, chunk...)
			if len(line) > max {
				return nil, true, err
			}
			return line, false, err
		default:
			if tooLong {
				return nil, true, nil
			}
			line = append(line, chunk...)
			if len(line) > max {
				return nil, true, nil
			}
			return line, false, nil
		}
	}
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
