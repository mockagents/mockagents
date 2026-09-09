package main

import (
	"strings"
	"testing"
)

// TestCorsOriginsFor is the audit M-06 default: explicit origins always win;
// otherwise single-tenant keeps the wildcard (nil) and multi-tenant gets the
// loopback GUI origins instead of `*` with cookie auth on.
func TestCorsOriginsFor(t *testing.T) {
	if got := corsOriginsFor(false, ""); got != nil {
		t.Fatalf("single-tenant default = %v, want nil (wildcard)", got)
	}
	if got := corsOriginsFor(true, ""); len(got) != 2 || got[0] != "http://localhost:3001" {
		t.Fatalf("multi-tenant default = %v", got)
	}
	got := corsOriginsFor(true, " https://a.example , https://b.example ,")
	if len(got) != 2 || got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("explicit list = %v", got)
	}
}

// TestCheckManageBind is the audit M-23 guard: unauthenticated management
// tools stay on loopback unless the operator says otherwise.
func TestCheckManageBind(t *testing.T) {
	ok := []struct{ transport, bind string }{
		{"http", "127.0.0.1"}, {"http", "::1"}, {"http", "localhost"}, {"stdio", "0.0.0.0"},
	}
	for _, c := range ok {
		if err := checkManageBind(c.transport, c.bind, false); err != nil {
			t.Errorf("%s %s: unexpected refusal: %v", c.transport, c.bind, err)
		}
	}
	for _, bind := range []string{"0.0.0.0", "", "10.0.0.5", "::"} {
		err := checkManageBind("http", bind, false)
		if err == nil || !strings.Contains(err.Error(), "--allow-remote-manage") {
			t.Errorf("bind %q: want refusal naming the override, got %v", bind, err)
		}
		if err := checkManageBind("http", bind, true); err != nil {
			t.Errorf("bind %q with override: %v", bind, err)
		}
	}
}
