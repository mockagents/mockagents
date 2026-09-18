package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentBounds_ValidateAndPut(t *testing.T) {
	for _, tc := range []struct {
		rate  string
		turn  int
		valid bool
	}{{"0", 1, true}, {"1", 2, true}, {"-0.1", 1, false}, {"1.1", 1, false}, {"0", 0, false}, {"0", -1, false}} {
		t.Run(fmt.Sprintf("rate=%s/turn=%d", tc.rate, tc.turn), func(t *testing.T) {
			body := fmt.Sprintf("apiVersion: mockagents/v1\nkind: Agent\nmetadata: {name: bounds}\nspec:\n  protocol: openai-chat-completions\n  tools:\n    - name: lookup\n      error_rate: %s\n  behavior:\n    scenarios:\n      - name: first\n        match: {turn_number: %d}\n        response: {content: hi}\n", tc.rate, tc.turn)
			r := httptest.NewRequest("POST", "/api/v1/config/validate", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/yaml")
			w := httptest.NewRecorder()
			NewValidateHandler().ServeHTTP(w, r)
			var result ValidateResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || result.OK != tc.valid {
				t.Fatalf("validate: %d %s", w.Code, w.Body.String())
			}
			srv, _ := newAgentWriteServer(t, t.TempDir())
			// PUT creates a missing agent; validation must agree with the preview.
			code, response := doReq(t, "PUT", srv.URL+"/api/v1/agents/bounds", "application/yaml", body)
			if tc.valid && code != 200 && code != 201 {
				t.Fatalf("put: %d %s", code, response)
			}
			if !tc.valid && code != 422 {
				t.Fatalf("invalid put: %d %s", code, response)
			}
		})
	}
}
