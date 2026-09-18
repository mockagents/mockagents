package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/stretchr/testify/require"
)

func TestResponsesAuthenticatedTenantBoundary(t *testing.T) {
	store := newRotateTestStore(t)
	keys := map[string]string{}
	for _, name := range []string{"a", "b"} {
		tenant, err := store.CreateTenant(t.Context(), name)
		require.NoError(t, err)
		key, err := store.CreateAPIKey(t.Context(), tenant.ID, "test", tenancy.RoleViewer)
		require.NoError(t, err)
		keys[name] = key.Plaintext
	}
	ts, _ := obsTestServer(t, func(c *Config) { c.TenancyStore = store }, testFullAgent("responses", "gpt-4o"))
	call := func(key, body string) (int, adapter.ResponsesResponse) {
		req := httptest.NewRequest("POST", ts.URL+"/v1/responses", strings.NewReader(body))
		req.RequestURI = ""
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		req.Header.Set("X-Mockagents-Tenant", "a")
		response, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer response.Body.Close()
		var out adapter.ResponsesResponse
		if response.StatusCode == 200 {
			require.NoError(t, json.NewDecoder(response.Body).Decode(&out))
		}
		return response.StatusCode, out
	}
	status, first := call(keys["a"], `{"model":"gpt-4o","input":"hello"}`)
	require.Equal(t, 200, status)
	for _, caller := range []string{"a", "b", ""} {
		status, _ := call(keys[caller], `{"model":"gpt-4o","input":"again","previous_response_id":"`+first.ID+`"}`)
		want := 404
		if caller == "a" {
			want = 200
		}
		require.Equal(t, want, status, caller)
	}
	status, unstored := call(keys["a"], `{"model":"gpt-4o","input":"hello","store":false}`)
	require.Equal(t, 200, status)
	status, _ = call(keys["a"], `{"model":"gpt-4o","input":"again","previous_response_id":"`+unstored.ID+`"}`)
	require.Equal(t, 404, status)
}
