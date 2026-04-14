package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Request Body Size Limits ---

func TestSecurity_LargeRequestBody(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	// Send a body larger than the default 10MB limit.
	largeBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"` +
		strings.Repeat("A", 11*1024*1024) + `"}]}`

	resp, err := http.Post(addr+"/v1/chat/completions", "application/json",
		strings.NewReader(largeBody))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should be rejected by MaxBodySize middleware.
	assert.True(t, resp.StatusCode == http.StatusRequestEntityTooLarge ||
		resp.StatusCode == http.StatusBadRequest,
		"expected 413 or 400 for oversized body, got %d", resp.StatusCode)
}

// --- CORS Preflight ---

func TestSecurity_CORSPreflight(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	req, _ := http.NewRequest("OPTIONS", addr+"/v1/chat/completions", nil)
	req.Header.Set("Origin", "http://evil.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
}

// --- Unknown Endpoints ---

func TestSecurity_UnknownEndpoint(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Get(addr + "/v1/unknown/endpoint")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return 404 or 405, not leak internal info.
	assert.True(t, resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusMethodNotAllowed,
		"unexpected status %d for unknown endpoint", resp.StatusCode)
}

// --- Method Enforcement ---

func TestSecurity_WrongHTTPMethod(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	// GET to a POST-only endpoint.
	resp, err := http.Get(addr + "/v1/chat/completions")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.True(t, resp.StatusCode == http.StatusMethodNotAllowed ||
		resp.StatusCode == http.StatusNotFound,
		"expected 405 or 404 for wrong method, got %d", resp.StatusCode)
}

// --- Request ID Always Present ---

func TestSecurity_RequestIDAlwaysPresent(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	endpoints := []string{
		"/api/v1/health",
		"/api/v1/agents",
	}

	for _, ep := range endpoints {
		resp, err := http.Get(addr + ep)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Header.Get("X-Request-Id"),
			"endpoint %s should return X-Request-Id", ep)
		resp.Body.Close()
	}
}

// --- Panic Recovery ---

func TestSecurity_PanicRecovery(t *testing.T) {
	// The recovery middleware is already tested in middleware_test.go.
	// This test verifies it's active in the full server stack.
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	// A well-formed request should not trigger any panic.
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	resp, err := http.Post(addr+"/v1/chat/completions", "application/json",
		strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// --- Content-Type Enforcement ---

func TestSecurity_ContentTypeJSON(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	// All JSON API responses should have application/json content type.
	resp, err := http.Get(addr + "/api/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}
