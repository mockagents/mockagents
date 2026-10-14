package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/recording"
)

// importedOnlyEndpoints are LLM paths the importers keep that the record and
// replay servers used not to mount (they mounted only chat completions and
// messages), so an imported interaction on one of them 404'd at the mux before
// the cassette was ever consulted.
var importedOnlyEndpoints = []string{"/v1/responses", "/v1/embeddings", "/v1/moderations"}

func TestReplayMux_ServesEveryImportedEndpoint(t *testing.T) {
	var b strings.Builder
	b.WriteString("interactions:\n")
	for _, p := range importedOnlyEndpoints {
		b.WriteString(`- request:
    method: POST
    uri: https://api.openai.com` + p + `
    body: '{"model":"m","input":"` + p + `"}'
  response:
    status: {code: 200, message: OK}
    headers:
      Content-Type: [application/json]
    body: {string: '{"served":"` + p + `"}'}
`)
	}
	b.WriteString("version: 1\n")

	interactions, res, err := recording.ImportVCR(strings.NewReader(b.String()), recording.ImportVCROpts{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != len(importedOnlyEndpoints) {
		t.Fatalf("imported %d interactions, want %d (skips: %v)", res.Imported, len(importedOnlyEndpoints), res.SkipReasons)
	}
	cass := recording.New("")
	if err := cass.AppendAll(interactions); err != nil {
		t.Fatalf("append: %v", err)
	}

	mux := newReplayMux(recording.NewReplay(cass), cass)
	for _, p := range importedOnlyEndpoints {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{"model":"m","input":"`+p+`"}`)))
		if rec.Code != http.StatusOK || rec.Header().Get("X-Mockagents-Replay") != "hit" ||
			!strings.Contains(rec.Body.String(), p) {
			t.Errorf("POST %s: status=%d replay=%q body=%q, want the imported interaction",
				p, rec.Code, rec.Header().Get("X-Mockagents-Replay"), rec.Body.String())
		}
	}
}

func TestRecordMux_RoutesEveryImportedEndpoint(t *testing.T) {
	var reached string
	proxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = r.URL.Path })
	mux := newRecordMux(proxy, recording.New(""))
	for _, p := range append([]string{"/v1/chat/completions", "/v1/messages"}, importedOnlyEndpoints...) {
		reached = ""
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`)))
		if reached != p {
			t.Errorf("POST %s never reached the recording proxy", p)
		}
	}
}

func TestValidateStrictReplay(t *testing.T) {
	tests := []struct {
		name     string
		strict   bool
		mode     recording.RecordMode
		upstream string
		apiKey   string
		wantErr  string
	}{
		{name: "disabled allows recording", mode: recording.RecordModeAll},
		{name: "strict replay only", strict: true, mode: recording.RecordModeNone},
		{name: "reject record mode", strict: true, mode: recording.RecordModeNewEpisodes, wantErr: "record-mode=none"},
		{name: "reject upstream", strict: true, mode: recording.RecordModeNone, upstream: "https://example.test", wantErr: "--upstream"},
		{name: "reject api key", strict: true, mode: recording.RecordModeNone, apiKey: "secret", wantErr: "--api-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStrictReplay(tt.strict, tt.mode, tt.upstream, tt.apiKey)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
