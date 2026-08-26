package main

import "testing"

func TestAIMockAgentName(t *testing.T) {
	for input, want := range map[string]string{
		"fixtures/My Agent.json": "my-agent",
		"!!!.json":               "aimock-agent",
	} {
		if got := aimockAgentName(input); got != want {
			t.Fatalf("aimockAgentName(%q)=%q want %q", input, got, want)
		}
	}
}
