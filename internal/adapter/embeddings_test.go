package adapter

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func doEmbeddings(t *testing.T, h *EmbeddingsHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleEmbeddings(rec, req)
	return rec
}

// --- vector math (white-box) ---

func TestEmbedding_Determinism(t *testing.T) {
	a := generateEmbedding("hello world", "text-embedding-3-small", 256)
	b := generateEmbedding("hello world", "text-embedding-3-small", 256)
	require.Equal(t, len(a), len(b))
	assert.Equal(t, a, b, "same input+model+dims must be byte-identical")
}

func TestEmbedding_DifferentInputs(t *testing.T) {
	a := generateEmbedding("alpha", "text-embedding-3-small", 64)
	b := generateEmbedding("beta", "text-embedding-3-small", 64)
	assert.NotEqual(t, a, b)
}

func TestEmbedding_DifferentModels(t *testing.T) {
	a := generateEmbedding("same text", "text-embedding-3-small", 64)
	b := generateEmbedding("same text", "text-embedding-3-large", 64)
	assert.NotEqual(t, a, b, "model name is part of the seed")
}

func TestEmbedding_UnitNorm(t *testing.T) {
	for _, dims := range []int{16, 256, 1536, 3072} {
		vec := generateEmbedding("normalize me", "text-embedding-3-large", dims)
		var sumSq float64
		for _, x := range vec {
			sumSq += float64(x) * float64(x)
		}
		norm := math.Sqrt(sumSq)
		assert.InDelta(t, 1.0, norm, 1e-3, "L2 norm should be ~1 for dims=%d", dims)
	}
}

func TestEmbedding_DefaultDimensions(t *testing.T) {
	assert.Equal(t, 1536, defaultDimensions("text-embedding-3-small"))
	assert.Equal(t, 3072, defaultDimensions("text-embedding-3-large"))
	assert.Equal(t, 1536, defaultDimensions("text-embedding-ada-002"))
	assert.Equal(t, 1536, defaultDimensions("some-unknown-model"))
	assert.Equal(t, 3072, defaultDimensions("TEXT-EMBEDDING-3-LARGE"), "case-insensitive")
}

func TestEmbedding_Base64RoundTrip(t *testing.T) {
	vec := generateEmbedding("round trip", "text-embedding-3-small", 8)
	enc := encodeBase64Embedding(vec)
	raw, err := base64.StdEncoding.DecodeString(enc)
	require.NoError(t, err)
	require.Len(t, raw, len(vec)*4, "4 bytes per float32")
	// First component decodes (little-endian) back to the same float32.
	got := math.Float32frombits(binary.LittleEndian.Uint32(raw[:4]))
	assert.Equal(t, vec[0], got)
}

// --- input parsing ---

func TestParseEmbeddingInputs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []embeddingInput
		err  bool
	}{
		{"string", `"hello"`, []embeddingInput{{seed: "hello"}}, false},
		{"string array", `["a","b"]`, []embeddingInput{{seed: "a"}, {seed: "b"}}, false},
		{"token array", `[101,102,103]`, []embeddingInput{{seed: "101,102,103", tokenCount: 3}}, false},
		{"token matrix", `[[1,2],[3,4]]`, []embeddingInput{{seed: "1,2", tokenCount: 2}, {seed: "3,4", tokenCount: 2}}, false},
		{"empty string", `""`, nil, true},
		{"empty array", `[]`, nil, true},
		{"empty element", `["a",""]`, nil, true},
		{"null element", `[null]`, nil, true},
		{"empty token row", `[[1],[]]`, nil, true},
		{"missing", ``, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseEmbeddingInputs(json.RawMessage(tc.raw))
			if tc.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestEmbedding_TokenCountUsage pins the fix for the pre-tokenized undercount:
// token arrays must report prompt_tokens equal to the id count, not a
// whitespace-heuristic estimate of the comma-joined string.
func TestEmbedding_TokenCountUsage(t *testing.T) {
	rec := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-3-small","input":[101,102,103],"dimensions":4}`)
	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 3, resp.Usage.PromptTokens, "pre-tokenized input reports the exact id count")

	rec2 := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-3-small","input":[[1,2],[3,4,5]],"dimensions":4}`)
	var resp2 EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, 5, resp2.Usage.PromptTokens, "token matrix reports the summed id count")
}

// TestHandleEmbeddings_BatchTooLarge pins the input-count DoS cap.
func TestHandleEmbeddings_BatchTooLarge(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"model":"text-embedding-3-small","input":[`)
	for i := 0; i < maxEmbeddingInputs+1; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`"x"`)
	}
	sb.WriteString(`]}`)
	rec := doEmbeddings(t, &EmbeddingsHandler{}, sb.String())
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- HTTP handler ---

func TestHandleEmbeddings_SingleString(t *testing.T) {
	rec := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-3-small","input":"hello"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, "text-embedding-3-small", resp.Model)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "embedding", resp.Data[0].Object)
	assert.Equal(t, 0, resp.Data[0].Index)
	// default dims for small = 1536
	vec := resp.Data[0].Embedding.([]any)
	assert.Len(t, vec, 1536)
	assert.Greater(t, resp.Usage.PromptTokens, 0)
	assert.Equal(t, resp.Usage.PromptTokens, resp.Usage.TotalTokens)
}

func TestHandleEmbeddings_StringArrayIndices(t *testing.T) {
	rec := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-3-small","input":["foo","bar","baz"],"dimensions":8}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 3)
	for i := range resp.Data {
		assert.Equal(t, i, resp.Data[i].Index)
		assert.Len(t, resp.Data[i].Embedding.([]any), 8)
	}
}

func TestHandleEmbeddings_TokenArrayAndMatrix(t *testing.T) {
	rec := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-3-small","input":[101,102,103],"dimensions":4}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)

	rec2 := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-3-small","input":[[1,2],[3,4]],"dimensions":4}`)
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	require.Len(t, resp2.Data, 2)
}

func TestHandleEmbeddings_DimensionsOverrideAndCap(t *testing.T) {
	// override down to 256
	rec := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-3-small","input":"x","dimensions":256}`)
	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Data[0].Embedding.([]any), 256)

	// request above the model max is capped to the model default (3072 for large)
	rec2 := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-3-large","input":"x","dimensions":99999}`)
	var resp2 EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Len(t, resp2.Data[0].Embedding.([]any), 3072)
}

func TestHandleEmbeddings_Base64(t *testing.T) {
	rec := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-3-small","input":"hello","encoding_format":"base64","dimensions":8}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// embedding is a base64 string, not an array
	s, ok := resp.Data[0].Embedding.(string)
	require.True(t, ok, "base64 embedding should decode to a string")
	raw, err := base64.StdEncoding.DecodeString(s)
	require.NoError(t, err)
	assert.Len(t, raw, 8*4)
}

func TestHandleEmbeddings_ModelEchoedAndNoCompletionTokens(t *testing.T) {
	rec := doEmbeddings(t, &EmbeddingsHandler{}, `{"model":"text-embedding-ada-002","input":"hello world"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `"model":"text-embedding-ada-002"`)
	assert.NotContains(t, body, "completion_tokens", "embeddings usage has no completion_tokens")

	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, EstimateTokens("hello world"), resp.Usage.PromptTokens)
}

func TestHandleEmbeddings_Errors(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
	}{
		{"missing model", `{"input":"hi"}`, http.StatusBadRequest},
		{"empty input", `{"model":"m","input":""}`, http.StatusBadRequest},
		{"empty array", `{"model":"m","input":[]}`, http.StatusBadRequest},
		{"empty array element", `{"model":"m","input":["a",""]}`, http.StatusBadRequest},
		{"null array element", `{"model":"m","input":[null]}`, http.StatusBadRequest},
		{"empty token row", `{"model":"m","input":[[]]}`, http.StatusBadRequest},
		{"zero dimensions", `{"model":"m","input":"hi","dimensions":0}`, http.StatusBadRequest},
		{"negative dimensions", `{"model":"m","input":"hi","dimensions":-5}`, http.StatusBadRequest},
		{"dimensions on ada-002", `{"model":"text-embedding-ada-002","input":"hi","dimensions":256}`, http.StatusBadRequest},
		{"bad encoding", `{"model":"m","input":"hi","encoding_format":"binary"}`, http.StatusBadRequest},
		{"malformed json", `{"model":`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doEmbeddings(t, &EmbeddingsHandler{}, tc.body)
			assert.Equal(t, tc.code, rec.Code)
			var errResp map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
			assert.NotNil(t, errResp["error"])
		})
	}
}

func TestHandleEmbeddings_OversizeBody413(t *testing.T) {
	huge := `{"model":"m","input":"` + strings.Repeat("a", maxDecodeBodyBytes+1024) + `"}`
	rec := doEmbeddings(t, &EmbeddingsHandler{}, huge)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// --- e2e through the mounted registry ---

func TestEmbeddingsRoute_MountedInRegistry(t *testing.T) {
	mux := http.NewServeMux()
	for _, a := range DefaultRegistry(testEngine()).Adapters() {
		for _, route := range a.Routes() {
			mux.HandleFunc(route.Pattern, route.Handler)
		}
	}
	req := httptest.NewRequest("POST", "/v1/embeddings",
		strings.NewReader(`{"model":"text-embedding-3-small","input":"mounted","dimensions":16}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Len(t, resp.Data[0].Embedding.([]any), 16)
}
