package adapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// AzureHandler serves the Azure OpenAI URL surface so an `AzureOpenAI()` SDK
// client runs unchanged against the mock. It owns no engine — it DELEGATES to
// the existing OpenAI chat-completions and embeddings handlers:
//
//	POST /openai/deployments/{deployment}/chat/completions   (classic surface)
//	POST /openai/deployments/{deployment}/embeddings
//	POST /openai/v1/chat/completions                         (new unified surface)
//	POST /openai/v1/embeddings
//
// On the classic deployment surface the model is the {deployment} path segment
// (the request body may omit "model"), so those routes inject the deployment as
// the model before delegating. The unified /openai/v1 surface carries the model
// in the body like standard OpenAI, so it delegates directly. The `api-version`
// query parameter is accepted and ignored (the mock is versionless).
type AzureHandler struct {
	Chat       *OpenAIHandler
	Embeddings *EmbeddingsHandler
}

// Name identifies this adapter in logs and diagnostics.
func (h *AzureHandler) Name() string { return "azure-openai" }

// Routes returns the Azure-compatible routes mounted through the adapter Registry.
func (h *AzureHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /openai/deployments/{deployment}/chat/completions", Handler: h.handleDeploymentChat},
		{Pattern: "POST /openai/deployments/{deployment}/embeddings", Handler: h.handleDeploymentEmbeddings},
		{Pattern: "POST /openai/v1/chat/completions", Handler: h.Chat.HandleChatCompletions},
		{Pattern: "POST /openai/v1/embeddings", Handler: h.Embeddings.HandleEmbeddings},
	}
}

func (h *AzureHandler) handleDeploymentChat(w http.ResponseWriter, r *http.Request) {
	if h.injectDeployment(w, r) {
		h.Chat.HandleChatCompletions(w, r)
	}
}

func (h *AzureHandler) handleDeploymentEmbeddings(w http.ResponseWriter, r *http.Request) {
	if h.injectDeployment(w, r) {
		h.Embeddings.HandleEmbeddings(w, r)
	}
}

// injectDeployment rewrites the request body so the {deployment} path segment
// becomes the model when the body omits it, then leaves r.Body replayable for
// the downstream handler. Returns false (after writing the error) on a malformed
// or oversized body.
func (h *AzureHandler) injectDeployment(w http.ResponseWriter, r *http.Request) bool {
	if err := ensureModel(r, r.PathValue("deployment")); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return false
	}
	return true
}

// ensureModel reads r.Body and, if it lacks a non-empty "model" field, injects
// deployment as the model; it then replaces r.Body with a buffered, replayable
// reader so the downstream handler can decode it normally. The body is
// size-capped (10 MiB) like every other adapter route — an oversize body
// surfaces as *http.MaxBytesError (mapped to 413 by the caller).
func ensureModel(r *http.Request, deployment string) error {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxDecodeBodyBytes))
	if err != nil {
		return err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	// A bare JSON `null` unmarshals into a nil map without error; reject it as a
	// 400 (rather than panicking on the assignment below). Other non-objects
	// (arrays, scalars) already fail the unmarshal into a map.
	if fields == nil {
		return errors.New("request body must be a JSON object")
	}

	if !hasModel(fields) {
		modelJSON, err := json.Marshal(deployment)
		if err != nil {
			return err
		}
		fields["model"] = modelJSON
		body, err = json.Marshal(fields)
		if err != nil {
			return err
		}
	}

	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return nil
}

// hasModel reports whether the decoded body already carries a non-empty model.
func hasModel(fields map[string]json.RawMessage) bool {
	raw, ok := fields["model"]
	if !ok {
		return false
	}
	var s string
	return json.Unmarshal(raw, &s) == nil && s != ""
}
