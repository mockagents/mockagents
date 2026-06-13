package adapter

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ensureModel (unit) ---

func TestEnsureModel(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		deployment string
		wantModel  string
		wantErr    bool
	}{
		{"inject when absent", `{"messages":[{"role":"user","content":"hi"}]}`, "gpt-4o", "gpt-4o", false},
		{"preserve when present", `{"model":"gpt-4o","messages":[]}`, "dep-name", "gpt-4o", false},
		{"body model differs wins", `{"model":"custom-model","messages":[]}`, "dep-name", "custom-model", false},
		{"empty model injects deployment", `{"model":"","messages":[]}`, "gpt-4o", "gpt-4o", false},
		{"malformed json", `not json`, "gpt-4o", "", true},
		{"empty body", ``, "gpt-4o", "", true},
		{"json null body", `null`, "gpt-4o", "", true}, // must not panic on the nil map
		{"json array body", `[1,2,3]`, "gpt-4o", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/openai/deployments/"+tc.deployment+"/chat/completions", strings.NewReader(tc.body))
			err := ensureModel(r, tc.deployment)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			b, _ := io.ReadAll(r.Body)
			var m map[string]any
			require.NoError(t, json.Unmarshal(b, &m))
			assert.Equal(t, tc.wantModel, m["model"])
		})
	}
}

func TestEnsureModel_PreservesOtherFields(t *testing.T) {
	r := httptest.NewRequest("POST", "/openai/deployments/gpt-4o/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],"stream":true,"temperature":0.5}`))
	require.NoError(t, ensureModel(r, "gpt-4o"))
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "gpt-4o", m["model"])
	assert.Equal(t, true, m["stream"])
	assert.InDelta(t, 0.5, m["temperature"], 1e-9)
	assert.Len(t, m["messages"], 1)
}

func TestEnsureModel_Oversize(t *testing.T) {
	big := `{"model":"x","pad":"` + strings.Repeat("a", maxDecodeBodyBytes+100) + `"}`
	r := httptest.NewRequest("POST", "/openai/deployments/d/chat/completions", strings.NewReader(big))
	err := ensureModel(r, "d")
	var maxErr *http.MaxBytesError
	assert.True(t, errors.As(err, &maxErr), "oversize body must surface MaxBytesError, got %v", err)
}

// --- e2e through the mounted registry ---

func azureMux() *http.ServeMux {
	mux := http.NewServeMux()
	for _, a := range DefaultRegistry(testEngine(testOpenAIAgent())).Adapters() {
		for _, route := range a.Routes() {
			mux.HandleFunc(route.Pattern, route.Handler)
		}
	}
	return mux
}

func azurePost(t *testing.T, mux *http.ServeMux, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAzure_DeploymentChat_ModelFromPath(t *testing.T) {
	// Body omits "model"; the {deployment} path segment supplies it. The
	// api-version query param must be ignored.
	rec := azurePost(t, azureMux(),
		"/openai/deployments/gpt-4o/chat/completions?api-version=2024-02-01",
		`{"messages":[{"role":"user","content":"hello"}]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "chat.completion", resp.Object)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.Equal(t, "Hi there!", *resp.Choices[0].Message.Content)
}

func TestAzure_DeploymentChat_BodyModelPreserved(t *testing.T) {
	rec := azurePost(t, azureMux(),
		"/openai/deployments/some-deployment/chat/completions?api-version=2024-05-01-preview",
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "gpt-4o", resp.Model)
}

func TestAzure_DeploymentEmbeddings(t *testing.T) {
	rec := azurePost(t, azureMux(),
		"/openai/deployments/text-embedding-3-small/embeddings?api-version=2024-02-01",
		`{"input":"hello"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, "text-embedding-3-small", resp.Model)
	require.Len(t, resp.Data, 1)
}

func TestAzure_V1Chat(t *testing.T) {
	rec := azurePost(t, azureMux(), "/openai/v1/chat/completions",
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "chat.completion", resp.Object)
}

func TestAzure_V1Embeddings(t *testing.T) {
	rec := azurePost(t, azureMux(), "/openai/v1/embeddings",
		`{"model":"text-embedding-3-small","input":"x"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
}

func TestAzure_V1Chat_MissingModel400(t *testing.T) {
	// The unified surface carries model in the body; absent -> the downstream
	// handler's own validation rejects it.
	rec := azurePost(t, azureMux(), "/openai/v1/chat/completions",
		`{"messages":[{"role":"user","content":"hi"}]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAzure_DeploymentChat_Oversize413(t *testing.T) {
	big := `{"pad":"` + strings.Repeat("a", maxDecodeBodyBytes+1024) + `"}`
	rec := azurePost(t, azureMux(), "/openai/deployments/gpt-4o/chat/completions", big)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestAzure_DeploymentChat_Malformed400(t *testing.T) {
	rec := azurePost(t, azureMux(), "/openai/deployments/gpt-4o/chat/completions", `{not json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAzure_DeploymentChat_NullBody400(t *testing.T) {
	// A bare JSON `null` is valid JSON but not an object — must be a clean 400,
	// never a nil-map panic (recovered as 500).
	rec := azurePost(t, azureMux(), "/openai/deployments/gpt-4o/chat/completions", `null`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
