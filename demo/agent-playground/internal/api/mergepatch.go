package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// decodeLoose reads a JSON object body without a fixed schema (merge patches).
func decodeLoose(r *http.Request, v *map[string]any) error {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return fmt.Errorf("request body exceeds %d bytes", maxBody)
		}
		return err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return errors.New("request body is required")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("body must be a JSON object (merge patch): %w", err)
	}
	return nil
}

// mergePatch applies an RFC 7386 JSON merge patch: objects merge
// recursively, null deletes a key, anything else replaces.
func mergePatch(target, patch any) any {
	p, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	t, ok := target.(map[string]any)
	if !ok {
		t = map[string]any{}
	}
	for k, v := range p {
		if v == nil {
			delete(t, k)
			continue
		}
		t[k] = mergePatch(t[k], v)
	}
	return t
}

// mergeInto applies patch to cur via JSON, then decodes the result strictly
// back into T, so unknown or mistyped fields are rejected.
func mergeInto[T any](cur T, patch map[string]any) (T, error) {
	var zero T
	raw, err := json.Marshal(cur)
	if err != nil {
		return zero, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return zero, err
	}
	merged, err := json.Marshal(mergePatch(doc, patch))
	if err != nil {
		return zero, err
	}
	dec := json.NewDecoder(strings.NewReader(string(merged)))
	dec.DisallowUnknownFields()
	var out T
	if err := dec.Decode(&out); err != nil {
		return zero, fmt.Errorf("patch does not fit the schema: %w", err)
	}
	return out, nil
}
