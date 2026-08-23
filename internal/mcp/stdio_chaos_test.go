package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func TestServeStdioWithFaultsForceErrorAndOff(t *testing.T) {
	zero := 0.0
	faults := types.MCPFaults{Rate: &zero, Error: true}
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":7,"method":"ping","mockagentsChaos":"error"}` + "\n" +
			`{"jsonrpc":"2.0","id":8,"method":"ping","mockagentsChaos":"off"}` + "\n",
	)
	var out bytes.Buffer
	if err := ServeStdioWithFaults(newTestMCPServer(), in, &out, faults); err != nil {
		t.Fatalf("ServeStdioWithFaults: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("responses=%d, want 2: %s", len(lines), out.String())
	}
	var forced Response
	if err := json.Unmarshal([]byte(lines[0]), &forced); err != nil {
		t.Fatalf("decode forced response: %v", err)
	}
	if forced.Error == nil || forced.Error.Code != -32000 || string(forced.ID) != "7" {
		t.Fatalf("forced response=%+v", forced)
	}
	data, ok := forced.Error.Data.(map[string]any)
	if !ok || data["chaos"] == nil {
		t.Fatalf("forced response lacks chaos metadata: %+v", forced.Error.Data)
	}
	var allowed Response
	if err := json.Unmarshal([]byte(lines[1]), &allowed); err != nil {
		t.Fatalf("decode allowed response: %v", err)
	}
	if allowed.Error != nil || string(allowed.ID) != "8" {
		t.Fatalf("off response=%+v", allowed)
	}
}

func TestStdioChaosSeededDecisionUsesMethodAndID(t *testing.T) {
	rate := 0.5
	faults := types.MCPFaults{Seed: 99, Rate: &rate, Error: true}
	frame := []byte(`{"jsonrpc":"2.0","id":"stable","method":"ping"}`)
	first, firstHandled := applyStdioChaos(frame, faults)
	for i := 0; i < 10; i++ {
		got, handled := applyStdioChaos(frame, faults)
		if handled != firstHandled || !bytes.Equal(got, first) {
			t.Fatalf("same method/id produced a different decision on iteration %d", i)
		}
	}
}

func TestStdioChaosNotificationDoesNotReceiveErrorResponse(t *testing.T) {
	faults := types.MCPFaults{Error: true}
	out, handled := applyStdioChaos([]byte(
		`{"jsonrpc":"2.0","method":"notifications/initialized","mockagentsChaos":"error"}`,
	), faults)
	if !handled || out != nil {
		t.Fatalf("notification chaos=(%s,%v), want nil,true", out, handled)
	}
}
