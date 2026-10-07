package cli

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/app"
)

// TestVerifyEndToEnd boots the whole playground with its embedded mockagents
// server, serves it on a real port, and runs the same checklist as
// `playground verify`: every agent, tool, workflow, retry/fallback path,
// human-review path, configuration change and mock surface.
func TestVerifyEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end verification skipped in -short mode")
	}
	a, err := app.New(app.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = a.Run(ctx, ln); close(done) }()
	defer func() { cancel(); <-done }()

	rep := Verify(context.Background(), NewClient("http://"+ln.Addr().String(), ""), func(c Check) {
		if !c.Passed {
			t.Errorf("[%s] %s: %s", c.Area, c.Name, c.Detail)
		}
	})
	t.Logf("%d checks passed, %d failed", rep.Passed, rep.Failed)
	if !rep.OK() {
		t.Fatal("verification failed")
	}
	if rep.Passed < 45 {
		t.Fatalf("only %d checks ran", rep.Passed)
	}
}

func TestKVPatch(t *testing.T) {
	p, err := kvPatch([]string{"tier=llm", "retry.max_retries=5", "stream=true", "system_prompt=be brief", "temperature=null"})
	if err != nil {
		t.Fatal(err)
	}
	if p["tier"] != "llm" || p["retry"].(map[string]any)["max_retries"] != float64(5) || p["stream"] != true || p["system_prompt"] != "be brief" || p["temperature"] != nil {
		t.Fatalf("patch %v", p)
	}
	if _, err := kvPatch([]string{"novalue"}); err == nil {
		t.Error("expected an error")
	}
}
