package adapter

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
)

// ProtocolOpenAIEmbeddings is the wire-protocol label recorded on interaction
// logs for the OpenAI Embeddings surface (POST /v1/embeddings).
const ProtocolOpenAIEmbeddings = "openai-embeddings"

// --- Request / response types ---

// EmbeddingsRequest is an OpenAI Embeddings API request. `input` is polymorphic
// (a string, an array of strings, a token-id array, or an array of token-id
// arrays) so it is captured as RawMessage and decoded by parseEmbeddingInputs.
type EmbeddingsRequest struct {
	Model          string          `json:"model"`
	Input          json.RawMessage `json:"input"`
	EncodingFormat string          `json:"encoding_format,omitempty"`
	Dimensions     *int            `json:"dimensions,omitempty"`
	User           string          `json:"user,omitempty"`
}

// EmbeddingsResponse is the OpenAI Embeddings API response (object "list").
type EmbeddingsResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  EmbeddingsUsage `json:"usage"`
}

// EmbeddingData is one element of the response `data` list. Embedding is `any`
// because it is a []float32 for encoding_format=float and a base64 string for
// encoding_format=base64.
type EmbeddingData struct {
	Object    string `json:"object"`
	Index     int    `json:"index"`
	Embedding any    `json:"embedding"`
}

// EmbeddingsUsage is the embeddings token-usage shape — note there is NO
// completion_tokens field (embeddings consume input tokens only).
type EmbeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// --- Handler ---

// EmbeddingsHandler serves the OpenAI Embeddings API (POST /v1/embeddings). It
// is intentionally engine-free: an embedding is a pure, deterministic transform
// of (input, model, dimensions), so there is no scenario, session, or agent to
// resolve. It works zero-config — any non-empty model returns stable vectors.
type EmbeddingsHandler struct{}

// Name identifies this adapter in logs and diagnostics.
func (h *EmbeddingsHandler) Name() string { return "openai-embeddings" }

// Routes returns the Embeddings route mounted through the adapter Registry.
func (h *EmbeddingsHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/embeddings", Handler: h.HandleEmbeddings},
	}
}

// HandleEmbeddings handles POST /v1/embeddings.
func (h *EmbeddingsHandler) HandleEmbeddings(w http.ResponseWriter, r *http.Request) {
	meta := engine.RequestMetaFromContext(r.Context())
	if meta != nil {
		meta.Protocol = ProtocolOpenAIEmbeddings
	}

	var req EmbeddingsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	encodeBase64Fmt := false
	switch req.EncodingFormat {
	case "", "float":
		// default: float array
	case "base64":
		encodeBase64Fmt = true
	default:
		writeError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("encoding_format must be 'float' or 'base64', got %q", req.EncodingFormat))
		return
	}

	inputs, err := parseEmbeddingInputs(req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	dims := defaultDimensions(req.Model)
	if req.Dimensions != nil {
		// `dimensions` is only honored by the v3 models; the real API rejects it
		// for legacy ada-002.
		if !supportsDimensions(req.Model) {
			writeError(w, http.StatusBadRequest, "invalid_request_error",
				fmt.Sprintf("dimensions is not supported by model %q", req.Model))
			return
		}
		if *req.Dimensions < 1 {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "dimensions must be a positive integer")
			return
		}
		// Reduce-only: a request never asks for more than the model's native
		// width (the real API caps it), but may ask for fewer.
		if *req.Dimensions < dims {
			dims = *req.Dimensions
		}
	}

	data := make([]EmbeddingData, len(inputs))
	promptTokens := 0
	for i, in := range inputs {
		vec := generateEmbedding(in.seed, req.Model, dims)
		var emb any = vec
		if encodeBase64Fmt {
			emb = encodeBase64Embedding(vec)
		}
		data[i] = EmbeddingData{Object: "embedding", Index: i, Embedding: emb}
		// Pre-tokenized inputs carry an exact token count; real text is
		// estimated. (EstimateTokens on a comma-joined id string would
		// undercount a token array as one "word".)
		if in.tokenCount > 0 {
			promptTokens += in.tokenCount
		} else {
			promptTokens += EstimateTokens(in.seed)
		}
	}

	if meta != nil {
		meta.Model = req.Model
	}

	writeJSON(w, http.StatusOK, EmbeddingsResponse{
		Object: "list",
		Data:   data,
		Model:  req.Model,
		Usage:  EmbeddingsUsage{PromptTokens: promptTokens, TotalTokens: promptTokens},
	})
}

// --- input parsing ---

// maxEmbeddingInputs caps the number of inputs in a single batch, matching the
// real OpenAI limit. Without it a body of many tiny elements (e.g. a 10 MiB
// token matrix of one-element rows) would expand to millions of high-dimension
// float vectors and OOM the process — the request-body byte cap alone does not
// bound that amplification.
const maxEmbeddingInputs = 2048

// embeddingInput is one resolved input: the seed text for the deterministic
// vector plus an exact tokenCount for pre-tokenized inputs (0 means "estimate
// the token count from the seed text").
type embeddingInput struct {
	seed       string
	tokenCount int
}

// parseEmbeddingInputs decodes the polymorphic `input` into a flat list of
// inputs (one embedding per element). It accepts: a bare string; an array of
// strings; a single token-id array ([]int); or an array of token-id arrays
// ([][]int). Token arrays are rendered to a stable canonical string for the
// deterministic seed and carry their exact id count. Empty elements/rows and
// batches over maxEmbeddingInputs are rejected, mirroring the real API.
func parseEmbeddingInputs(raw json.RawMessage) ([]embeddingInput, error) {
	if len(raw) == 0 {
		return nil, errors.New("input is required")
	}

	// string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil, errors.New("input must not be empty")
		}
		return []embeddingInput{{seed: s}}, nil
	}

	// []string
	var ss []string
	if err := json.Unmarshal(raw, &ss); err == nil {
		if len(ss) == 0 {
			return nil, errors.New("input array must not be empty")
		}
		if err := checkBatchSize(len(ss)); err != nil {
			return nil, err
		}
		out := make([]embeddingInput, len(ss))
		for i, e := range ss {
			if e == "" {
				return nil, errors.New("input must not contain empty strings")
			}
			out[i] = embeddingInput{seed: e}
		}
		return out, nil
	}

	// []int — a single pre-tokenized input
	var toks []int64
	if err := json.Unmarshal(raw, &toks); err == nil {
		if len(toks) == 0 {
			return nil, errors.New("input array must not be empty")
		}
		return []embeddingInput{{seed: tokensToString(toks), tokenCount: len(toks)}}, nil
	}

	// [][]int — multiple pre-tokenized inputs
	var matrix [][]int64
	if err := json.Unmarshal(raw, &matrix); err == nil {
		if len(matrix) == 0 {
			return nil, errors.New("input array must not be empty")
		}
		if err := checkBatchSize(len(matrix)); err != nil {
			return nil, err
		}
		out := make([]embeddingInput, len(matrix))
		for i, row := range matrix {
			if len(row) == 0 {
				return nil, errors.New("input must not contain empty token arrays")
			}
			out[i] = embeddingInput{seed: tokensToString(row), tokenCount: len(row)}
		}
		return out, nil
	}

	return nil, errors.New("input must be a string, an array of strings, or token-id array(s)")
}

// checkBatchSize rejects an input batch larger than maxEmbeddingInputs.
func checkBatchSize(n int) error {
	if n > maxEmbeddingInputs {
		return fmt.Errorf("input array must not exceed %d elements", maxEmbeddingInputs)
	}
	return nil
}

// tokensToString renders a token-id array to a stable comma-joined string used
// as the seed material for a pre-tokenized input.
func tokensToString(toks []int64) string {
	parts := make([]string, len(toks))
	for i, t := range toks {
		parts[i] = strconv.FormatInt(t, 10)
	}
	return strings.Join(parts, ",")
}

// --- vector generation ---

// defaultDimensions returns a model's native embedding width. Unknown models
// default to 1536 (the most common size) so the endpoint is zero-config.
func defaultDimensions(model string) int {
	switch strings.ToLower(model) {
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-3-small", "text-embedding-ada-002":
		return 1536
	default:
		return 1536
	}
}

// supportsDimensions reports whether a model honors the `dimensions` parameter.
// The real API supports it on the v3 models and rejects it on legacy ada-002.
// Unknown/custom model names are allowed so the mock stays zero-config (a user
// testing an arbitrary embedder name can still shrink its output).
func supportsDimensions(model string) bool {
	return strings.ToLower(model) != "text-embedding-ada-002"
}

// generateEmbedding returns a deterministic, L2-normalized embedding for
// (text, model, dims). The seed is an FNV-1a hash of the three, so the same
// triple always yields byte-identical vectors (across restarts and processes),
// while different text/model/dims diverge. Values are drawn from a normal
// distribution then unit-normalized, mirroring real embedding-space statistics
// (cosine similarity is meaningful; magnitudes are ~1).
func generateEmbedding(text, model string, dims int) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(model))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(text))
	_, _ = h.Write([]byte{0})
	var d [8]byte
	binary.LittleEndian.PutUint64(d[:], uint64(dims))
	_, _ = h.Write(d[:])

	// PCG with the hash as the low seed word + a fixed golden-ratio high word
	// for stream separation. math/rand/v2's PCG is spec'd deterministic.
	rng := rand.New(rand.NewPCG(h.Sum64(), 0x9e3779b97f4a7c15))

	vec := make([]float32, dims)
	for i := range vec {
		vec[i] = float32(rng.NormFloat64())
	}

	// L2-normalize over the stored float32 values so the OUTPUT vector has unit
	// norm (computing the norm from the float64 draws would leave float32
	// rounding error in the result).
	var sumSq float64
	for _, x := range vec {
		sumSq += float64(x) * float64(x)
	}
	if norm := math.Sqrt(sumSq); norm > 1e-9 {
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	}
	return vec
}

// encodeBase64Embedding serializes a vector as little-endian IEEE-754 float32
// bytes, base64-encoded — the wire form OpenAI returns for
// encoding_format=base64.
func encodeBase64Embedding(vec []float32) string {
	buf := make([]byte, len(vec)*4)
	for i, f := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return base64.StdEncoding.EncodeToString(buf)
}
