package mcp

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// conformanceDef builds a server definition with the content shapes the
// MCP conformance suite probes: an embedded-resource tool block, an
// audio block, a subscribable resource, and a prompt that embeds a
// resource by argument.
func conformanceDef() *types.MCPServerDefinition {
	return &types.MCPServerDefinition{
		APIVersion: types.AgentAPIVersion,
		Kind:       types.MCPServerKind,
		Metadata:   types.Metadata{Name: "mcp-conformance"},
		Spec: types.MCPServerSpec{
			Capabilities: types.MCPCapabilities{Tools: true, Resources: true, Prompts: true},
			Tools: []types.MCPTool{
				{
					Name: "test_embedded_resource",
					Responses: []types.MCPToolResponse{{
						Default: true,
						Content: []types.MCPContentBlock{{
							Type:     "resource",
							URI:      "test://embedded-resource",
							MimeType: "text/plain",
							Text:     "This is an embedded resource content.",
						}},
					}},
				},
				{
					Name: "test_audio_content",
					Responses: []types.MCPToolResponse{{
						Default: true,
						Content: []types.MCPContentBlock{{
							Type:     "audio",
							MimeType: "audio/wav",
							Data:     "QUJD",
						}},
					}},
				},
			},
			Resources: []types.MCPResource{{
				URI: "test://watched", Name: "Watched", MimeType: "text/plain", Text: "x",
			}},
			Prompts: []types.MCPPrompt{{
				Name:      "test_prompt_with_embedded_resource",
				Arguments: []types.MCPPromptArg{{Name: "resourceUri", Required: true}},
				Messages: []types.MCPPromptMessage{{
					Role: "user",
					Content: types.MCPContentBlock{
						Type:     "resource",
						URI:      "{{resourceUri}}",
						MimeType: "text/plain",
						Text:     "Embedded resource content for testing.",
					},
				}},
			}},
		},
	}
}

// TestEmbeddedResourceWireShape asserts a type:resource content block is
// emitted as an MCP EmbeddedResource — uri/mimeType/text nested under a
// "resource" object, not flat on the block.
func TestEmbeddedResourceWireShape(t *testing.T) {
	srv := NewServer(conformanceDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test_embedded_resource"}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)

	// The content block must carry a nested resource object…
	if !bytes.Contains(body, []byte(`"resource":{`)) {
		t.Errorf("embedded resource not nested under \"resource\": %s", body)
	}
	// …and decode to the EmbeddedResource shape.
	var decoded struct {
		Content []struct {
			Type     string `json:"type"`
			URI      string `json:"uri"`
			Resource *struct {
				URI      string `json:"uri"`
				MimeType string `json:"mimeType"`
				Text     string `json:"text"`
			} `json:"resource"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	c := decoded.Content[0]
	if c.Type != "resource" {
		t.Fatalf("type = %q, want resource", c.Type)
	}
	if c.URI != "" {
		t.Errorf("uri must not be flat on the block, got %q", c.URI)
	}
	if c.Resource == nil {
		t.Fatal("nested resource object missing")
	}
	if c.Resource.URI != "test://embedded-resource" || c.Resource.Text != "This is an embedded resource content." || c.Resource.MimeType != "text/plain" {
		t.Errorf("nested resource fields wrong: %+v", c.Resource)
	}
}

// TestAudioContentWireShape asserts an audio block marshals as
// {type,data,mimeType} with no stray nested object.
func TestAudioContentWireShape(t *testing.T) {
	srv := NewServer(conformanceDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test_audio_content"}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	if bytes.Contains(body, []byte(`"resource"`)) {
		t.Errorf("audio block must not nest a resource: %s", body)
	}
	var decoded struct {
		Content []struct {
			Type     string `json:"type"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	c := decoded.Content[0]
	if c.Type != "audio" || c.Data != "QUJD" || c.MimeType != "audio/wav" {
		t.Errorf("audio block wrong: %+v", c)
	}
}

// TestTextBlockUnaffected guards the alias-marshal path: a plain text
// block still emits just {type,text}.
func TestTextBlockUnaffected(t *testing.T) {
	b := types.MCPContentBlock{Type: "text", Text: "hi"}
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"type":"text","text":"hi"}` {
		t.Errorf("text block marshal = %s", out)
	}
}

// TestResourcesSubscribeLifecycle covers subscribe → tracked → empty
// result → unsubscribe → cleared, plus the missing-uri error.
func TestResourcesSubscribeLifecycle(t *testing.T) {
	srv := NewServer(conformanceDef())

	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":1,"method":"resources/subscribe","params":{"uri":"test://watched"}}`)
	if resp.Error != nil {
		t.Fatalf("subscribe error: %v", resp.Error)
	}
	if body, _ := json.Marshal(resp.Result); string(body) != `{}` {
		t.Errorf("subscribe result = %s, want {}", body)
	}
	if !srv.Subscribed("test://watched") {
		t.Error("uri not tracked after subscribe")
	}

	// Unsubscribe clears it; a second unsubscribe is a no-op.
	for i := 0; i < 2; i++ {
		resp = mustHandle(t, srv, `{"jsonrpc":"2.0","id":2,"method":"resources/unsubscribe","params":{"uri":"test://watched"}}`)
		if resp.Error != nil {
			t.Fatalf("unsubscribe error: %v", resp.Error)
		}
	}
	if srv.Subscribed("test://watched") {
		t.Error("uri still tracked after unsubscribe")
	}

	// Missing uri is a clear invalid-params error, not a panic.
	resp = mustHandle(t, srv, `{"jsonrpc":"2.0","id":3,"method":"resources/subscribe","params":{}}`)
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Errorf("missing uri should be InvalidParams, got %+v", resp.Error)
	}
}

// TestInitializeAdvertisesSubscribe confirms the resources capability
// now carries subscribe:true.
func TestInitializeAdvertisesSubscribe(t *testing.T) {
	srv := NewServer(conformanceDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	if !bytes.Contains(body, []byte(`"subscribe":true`)) {
		t.Errorf("resources capability missing subscribe:true: %s", body)
	}
}

// TestPromptEmbedsResourceByArg confirms {{arg}} is interpolated into a
// resource block's URI (not only into text blocks), and the wire shape
// is a nested EmbeddedResource.
func TestPromptEmbedsResourceByArg(t *testing.T) {
	srv := NewServer(conformanceDef())
	resp := mustHandle(t, srv, `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"test_prompt_with_embedded_resource","arguments":{"resourceUri":"test://supplied"}}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	var decoded struct {
		Messages []struct {
			Content struct {
				Type     string `json:"type"`
				Resource *struct {
					URI  string `json:"uri"`
					Text string `json:"text"`
				} `json:"resource"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	c := decoded.Messages[0].Content
	if c.Type != "resource" || c.Resource == nil {
		t.Fatalf("expected nested resource content, got %s", body)
	}
	if c.Resource.URI != "test://supplied" {
		t.Errorf("resource uri = %q, want interpolated test://supplied", c.Resource.URI)
	}
}
