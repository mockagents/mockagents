// Command contractcheck generates and verifies source-derived API/configuration schemas.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/server"
	"github.com/mockagents/mockagents/internal/types"
)

type schema = map[string]any

type builder struct {
	defs map[string]any
	tag  string
}

func (b *builder) shape(t reflect.Type) schema {
	if t == reflect.TypeFor[json.RawMessage]() {
		return schema{}
	}
	if t.Kind() == reflect.Pointer {
		return schema{"anyOf": []any{b.shape(t.Elem()), schema{"type": "null"}}}
	}
	switch t.Kind() {
	case reflect.Interface:
		return schema{}
	case reflect.String:
		return schema{"type": "string"}
	case reflect.Bool:
		return schema{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return schema{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return schema{"type": "number"}
	case reflect.Slice, reflect.Array:
		return schema{"type": []string{"array", "null"}, "items": b.shape(t.Elem())}
	case reflect.Map:
		return schema{"type": []string{"object", "null"}, "additionalProperties": b.shape(t.Elem())}
	case reflect.Struct:
		key := t.Name()
		if key != "" {
			key = t.PkgPath()[strings.LastIndex(t.PkgPath(), "/")+1:] + "." + key
			if _, ok := b.defs[key]; ok {
				return schema{"$ref": "#/$defs/" + key}
			}
			b.defs[key] = schema{}
		}
		props := schema{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get(b.tag)
			if f.PkgPath != "" || tag == "-" {
				continue
			}
			parts := strings.Split(tag, ",")
			name := parts[0]
			if name == "" {
				name = f.Name
			}
			if f.Anonymous {
				panic("embedded type needs an explicit schema: " + t.String())
			}
			props[name] = b.shape(f.Type)
			if !strings.Contains(tag, "omitempty") && b.tag == "json" {
				required = append(required, name)
			}
		}
		out := schema{"type": "object", "properties": props}
		if len(required) > 0 {
			out["required"] = required
		}
		if b.tag == "yaml" {
			out["additionalProperties"] = false
		}
		if key != "" {
			out["x-go-type"] = t.PkgPath() + "." + t.Name()
			b.defs[key] = out
			return schema{"$ref": "#/$defs/" + key}
		}
		return out
	default:
		panic("unsupported type " + t.String())
	}
}
func (b *builder) def(name string) schema { return b.defs[name].(map[string]any) }
func (b *builder) prop(name, field string) schema {
	return b.def(name)["properties"].(map[string]any)[field].(map[string]any)
}
func (b *builder) set(name, field string, value schema) {
	b.def(name)["properties"].(map[string]any)[field] = value
}
func str() schema               { return schema{"type": "string"} }
func array(items schema) schema { return schema{"type": "array", "items": items} }
func ref(name string) schema    { return schema{"$ref": "#/$defs/" + name} }
func obj(props schema, required ...string) schema {
	return schema{"type": "object", "properties": props, "required": required}
}
func generated() map[string][]byte {
	outputs := map[string][]byte{}
	emit := func(path string, doc schema) {
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			panic(err)
		}
		outputs[path] = append(data, '\n')
	}
	b := &builder{defs: map[string]any{}, tag: "json"}
	for _, v := range []any{adapter.ResponsesRequest{}, adapter.ResponsesResponse{}, adapter.OllamaChatRequest{}, adapter.OllamaChatResponse{}, adapter.BedrockConverseRequest{}, adapter.BedrockConverseResponse{}, adapter.GeminiRequest{}, adapter.GeminiResponse{}, adapter.EmbeddingsRequest{}, adapter.EmbeddingsResponse{}, adapter.ModerationsRequest{}, adapter.ModerationsResponse{}, server.PipelineRunRequest{}, engine.PipelineResult{}, config.ValidationError{}, types.SearchResult{}} {
		b.shape(reflect.TypeOf(v))
	}
	// Decode-time requiredness is independent of Go's output serialization tags.
	b.def("adapter.ResponsesRequest")["required"] = []string{"model", "input"}
	b.set("adapter.ResponsesRequest", "input", schema{"oneOf": []any{str(), array(schema{"type": "object"})}, "description": "String or typed message/function_call_output/function_call items; parsed by parseResponsesInput."})
	b.set("adapter.ResponsesRequest", "conversation", schema{"oneOf": []any{str(), obj(schema{"id": str()}, "id"), schema{"type": "null"}}, "description": "Mutually exclusive with previous_response_id. Conversation items persist even when store is false."})
	b.prop("adapter.ResponsesRequest", "store")["description"] = "Defaults true; false disables standalone history retention. Previous response IDs are tenant-scoped and globally FIFO bounded to 1024 entries."
	b.prop("adapter.ResponsesRequest", "model")["minLength"] = 1
	b.def("server.PipelineRunRequest")["additionalProperties"] = false
	b.prop("server.PipelineRunRequest", "input")["pattern"] = "\\S"
	b.def("adapter.OllamaChatRequest")["required"] = []string{"model", "messages"}
	b.prop("adapter.OllamaChatRequest", "stream")["description"] = "Defaults true; false selects a single JSON response."
	b.def("adapter.BedrockConverseRequest")["required"] = []string{"messages"}
	b.def("adapter.GeminiRequest")["required"] = []string{"contents"}
	b.def("adapter.EmbeddingsRequest")["required"] = []string{"model", "input"}
	b.set("adapter.EmbeddingsRequest", "input", schema{"oneOf": []any{str(), array(str()), array(schema{"type": "integer"}), array(array(schema{"type": "integer"}))}})
	b.set("adapter.EmbeddingsRequest", "encoding_format", schema{"type": "string", "enum": []string{"", "float", "base64"}})
	b.set("adapter.ModerationsRequest", "input", schema{"oneOf": []any{str(), array(str()), array(schema{"type": "object"})}})
	b.def("adapter.ModerationsRequest")["required"] = []string{"input"}
	b.defs["PipelineRunError"] = obj(schema{"error": str(), "code": schema{"enum": []string{"missing_dependency", "invalid_pipeline", "node_failed"}}, "result": ref("engine.PipelineResult")}, "error")
	b.defs["QuotaResult"] = obj(schema{"tenant_id": str(), "limits": obj(schema{"rate_per_sec": schema{"type": "number", "minimum": 0}, "rate_burst": schema{"type": "integer", "minimum": 0}, "monthly_spend_usd": schema{"type": "number", "minimum": 0}}, "rate_per_sec", "rate_burst", "monthly_spend_usd"), "usage": obj(schema{"month": str(), "spend_usd": schema{"type": "number"}}, "month", "spend_usd")}, "tenant_id", "limits", "usage")
	b.defs["QuotaUpdateResult"] = obj(schema{"tenant_id": str(), "limits": b.defs["QuotaResult"].(map[string]any)["properties"].(map[string]any)["limits"]}, "tenant_id", "limits")
	b.defs["AuthenticationError"] = obj(schema{"error": obj(schema{"type": str(), "message": str()}, "type", "message")}, "error")
	b.defs["TavilyRequest"] = obj(schema{"query": schema{"type": "string", "pattern": "\\S"}, "search_depth": schema{"enum": []string{"", "basic", "advanced", "fast", "ultra-fast"}}, "topic": str(), "max_results": schema{"type": "integer", "minimum": 0, "maximum": 20, "description": "0 or omitted selects 5."}, "include_answer": schema{}, "include_raw_content": schema{}, "include_domains": schema{"type": "array", "items": str(), "maxItems": 300}, "exclude_domains": schema{"type": "array", "items": str(), "maxItems": 150}, "time_range": schema{"enum": []string{"", "day", "d", "week", "w", "month", "m", "year", "y"}}, "start_date": str(), "end_date": str()}, "query")
	b.defs["TavilyResponse"] = obj(schema{"query": str(), "answer": str(), "images": array(schema{}), "results": array(ref("types.SearchResult")), "response_time": schema{"type": "number"}, "usage": obj(schema{"credits": schema{"type": "integer"}}, "credits"), "request_id": str()}, "query", "answer", "images", "results", "response_time", "usage", "request_id")
	b.defs["CohereRequest"] = obj(schema{"model": schema{"type": "string", "pattern": "\\S"}, "query": schema{"type": "string", "pattern": "\\S"}, "documents": schema{"type": "array", "items": str(), "minItems": 1, "maxItems": 1000}, "top_n": schema{"type": "integer", "minimum": 1}}, "model", "query", "documents")
	b.defs["CohereResponse"] = obj(schema{"id": str(), "results": array(obj(schema{"index": schema{"type": "integer"}, "relevance_score": schema{"type": "number", "minimum": 0, "maximum": 1}}, "index", "relevance_score")), "meta": obj(schema{"api_version": obj(schema{"version": str(), "is_experimental": schema{"type": "boolean"}}, "version", "is_experimental"), "billed_units": obj(schema{"search_units": schema{"type": "integer"}}, "search_units")}, "api_version", "billed_units")}, "id", "results", "meta")
	emit("docs/api-models.json", schema{"$schema": "https://json-schema.org/draft/2020-12/schema", "title": "MockAgents supplemental wire contracts", "description": "Generated by go run ./tools/contractcheck -write. Go tags define shape; explicit overlays define validation. Free-form polymorphic fields retain provider extensions; handler semantics remain documented in OpenAPI.", "$defs": b.defs})
	for _, v := range []any{types.A2AServerDefinition{}, types.SearchServiceDefinition{}} {
		c := &builder{defs: map[string]any{}, tag: "yaml"}
		root := c.shape(reflect.TypeOf(v))
		kind := "A2AServer"
		file := "a2aserver"
		if _, ok := v.(types.SearchServiceDefinition); ok {
			kind = "SearchService"
			file = "searchservice"
		}
		name := "types." + kind + "Definition"
		c.def(name)["required"] = []string{"apiVersion", "kind", "metadata", "spec"}
		c.set(name, "apiVersion", schema{"const": "mockagents/v1"})
		c.set(name, "kind", schema{"const": kind})
		c.def("types.Metadata")["required"] = []string{"name"}
		c.set("types.Metadata", "name", schema{"type": "string", "pattern": "^[a-z0-9]+(-[a-z0-9]+)*$"})
		configure(c, kind)
		root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
		root["$id"] = "https://mockagents.dev/schema/v1/" + file + ".json"
		root["title"] = "MockAgents " + kind + " Definition"
		root["$defs"] = c.defs
		emit("schema/mockagents-v1-"+file+".json", root)
	}
	return outputs
}
func main() {
	write := flag.Bool("write", false, "regenerate checked-in schema artifacts")
	flag.Parse()
	for path, want := range generated() {
		if *write {
			if err := os.WriteFile(path, want, 0644); err != nil {
				panic(err)
			}
		} else {
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
				fmt.Fprintln(os.Stderr, path+" is stale; run go run ./tools/contractcheck -write")
				os.Exit(1)
			}
		}
	}
	fmt.Println("contractcheck: wire schemas and A2AServer/SearchService schemas match source")
}
