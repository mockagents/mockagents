package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGeneratedSchemasCurrent(t *testing.T) {
	t.Chdir("../..")
	for path, want := range generated() {
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, string(want), strings.ReplaceAll(string(got), "\r\n", "\n"), path)
	}
}
func compiler(t *testing.T) *jsonschema.Compiler {
	t.Helper()
	c := jsonschema.NewCompiler()
	for path, body := range generated() {
		var doc any
		require.NoError(t, json.Unmarshal(body, &doc))
		require.NoError(t, c.AddResource(path, doc))
		if path == "docs/api-models.json" {
			for name := range doc.(map[string]any)["$defs"].(map[string]any) {
				_, err := c.Compile(path + "#/$defs/" + name)
				require.NoError(t, err, name)
			}
		}
	}
	return c
}
func TestConfigurationSchemasAndBounds(t *testing.T) {
	t.Chdir("../..")
	c := compiler(t)
	schemas := map[string]*jsonschema.Schema{}
	for _, kind := range []string{"A2AServer", "SearchService"} {
		s, err := c.Compile("schema/mockagents-v1-" + strings.ToLower(kind) + ".json")
		require.NoError(t, err)
		schemas[kind] = s
	}
	count := 0
	require.NoError(t, filepath.WalkDir("examples", func(path string, d os.DirEntry, err error) error {
		if d != nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if err != nil {
			return err
		}
		if d.IsDir() || !(strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc map[string]any
		if yaml.Unmarshal(body, &doc) != nil {
			return nil
		}
		kind, _ := doc["kind"].(string)
		if s := schemas[kind]; s != nil {
			count++
			require.Empty(t, config.ValidateBytes(body).Errors, path)
			require.NoError(t, s.Validate(doc), path)
		}
		return nil
	}))
	require.Positive(t, count)
	for _, kind := range []string{"A2AServer", "SearchService"} {
		for _, tc := range []struct {
			field string
			value any
			valid bool
		}{{"latency_ms", 0, true}, {"latency_ms", 60000, true}, {"latency_ms", 60001, false}, {"latency_ms", -1, false}, {"rate", 0.0, true}, {"rate", 1.0, true}, {"rate", 1.1, false}, {"status_code", 0, true}, {"status_code", 399, false}, {"status_code", 400, true}, {"status_code", 599, true}, {"status_code", 600, false}} {
			t.Run(kind+"/"+tc.field+"/"+fmtValue(tc.value), func(t *testing.T) {
				spec := map[string]any{"faults": map[string]any{tc.field: tc.value}}
				if kind == "A2AServer" {
					spec["card"] = map[string]any{"name": "test"}
				} else {
					spec["provider"] = "tavily"
				}
				doc := map[string]any{"apiVersion": "mockagents/v1", "kind": kind, "metadata": map[string]any{"name": "test"}, "spec": spec}
				body, err := json.Marshal(doc)
				require.NoError(t, err)
				require.Equal(t, tc.valid, len(config.ValidateBytes(body).Errors) == 0)
				require.Equal(t, tc.valid, schemas[kind].Validate(doc) == nil)
			})
		}
	}
}
func fmtValue(v any) string { b, _ := json.Marshal(v); return string(b) }
func TestOpenAPIExamplesAndLiveProviderResponses(t *testing.T) {
	t.Chdir("../..")
	c := compiler(t)
	body, err := os.ReadFile("docs/api-spec.yaml")
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(body, &spec))
	require.NoError(t, c.AddResource("docs/api-spec.yaml", spec))
	registry := engine.NewAgentRegistry()
	registry.Register(&types.AgentDefinition{APIVersion: types.AgentAPIVersion, Kind: types.AgentKind, Metadata: types.Metadata{Name: "contracts"}, Spec: types.AgentSpec{Protocol: "openai-chat-completions", Model: "gpt-4o", Behavior: types.BehaviorConfig{Scenarios: []types.Scenario{{Name: "default", Response: types.ScenarioResponse{Content: "hello"}}}}}})
	eng := engine.NewEngine(registry, state.NewMemoryStore(time.Minute), slog.Default())
	mux := http.NewServeMux()
	for _, a := range adapter.DefaultRegistry(eng).Adapters() {
		for _, r := range a.Routes() {
			mux.HandleFunc(r.Pattern, r.Handler)
		}
	}
	examples, live := 0, 0
	for path, raw := range spec["paths"].(map[string]any) {
		for method, rawOp := range raw.(map[string]any) {
			op, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			rb, ok := op["requestBody"].(map[string]any)
			if !ok {
				continue
			}
			content, _ := rb["content"].(map[string]any)
			media, ok := content["application/json"].(map[string]any)
			if !ok {
				continue
			}
			example, ok := media["example"]
			if !ok {
				continue
			}
			examples++
			pointer := "docs/api-spec.yaml#/paths/" + strings.ReplaceAll(strings.ReplaceAll(path, "~", "~0"), "/", "~1") + "/" + method
			requestSchema, err := c.Compile(pointer + "/requestBody/content/application~1json/schema")
			require.NoError(t, err, path)
			require.NoError(t, requestSchema.Validate(example), path)
			if strings.HasPrefix(path, "/api/v1/") || strings.HasSuffix(path, "converse-stream") {
				continue
			}
			url := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(path, "{modelmethod}", "gpt-4o:generateContent"), "{modelId}", "gpt-4o"), "{deployment}", "gpt-4o")
			payload, err := json.Marshal(example)
			require.NoError(t, err)
			r := httptest.NewRequest(strings.ToUpper(method), url, bytes.NewReader(payload))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			require.Equal(t, 200, w.Code, path+": "+w.Body.String())
			var response any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), path)
			responseSchema, err := c.Compile(pointer + "/responses/200/content/application~1json/schema")
			require.NoError(t, err, path)
			require.NoError(t, responseSchema.Validate(response), path)
			live++
		}
	}
	require.GreaterOrEqual(t, examples, 14)
	require.GreaterOrEqual(t, live, 12)
}
