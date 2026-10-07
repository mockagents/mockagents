package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/router"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/tools"
)

func load(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Load("../../config/playground.json", tools.NewRegistry().Names())
	if err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
	return c
}

func TestDefaultConfigIsValidAndComplete(t *testing.T) {
	c := load(t)
	if len(c.Agents) != 23 {
		t.Errorf("agents = %d, want 23", len(c.Agents))
	}
	for _, a := range c.Agents {
		for _, m := range a.Models {
			if _, ok := c.Pricing[m.Model]; !ok {
				t.Errorf("agent %s model %s has no price", a.Name, m.Model)
			}
		}
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	_, err := config.Parse([]byte(`{"defaults":{"retyr":{}}}`), nil)
	if err == nil || !strings.Contains(err.Error(), "retyr") {
		t.Fatalf("typo must be rejected, got %v", err)
	}
}

func TestValidationFieldErrors(t *testing.T) {
	c := load(t)
	c.Defaults.Retry.MaxRetries = 6
	c.Defaults.Review.TimeoutMS = c.Defaults.RunTimeoutMS
	c.Agents[0].Tier = "xl"
	c.Agents[1].Tools = []string{"rm_rf"}
	err := c.Validate(tools.NewRegistry().Names())
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	paths := map[string]bool{}
	for _, f := range ve.Fields {
		paths[f.Path] = true
	}
	for _, p := range []string{"defaults.retry", "defaults.review.timeout_ms", "agents[0].tier", "agents[1].tools"} {
		if !paths[p] {
			t.Errorf("missing field error for %s (got %v)", p, ve.Fields)
		}
	}
}

func TestWatchdogStallMustExceedLongestSilentWait(t *testing.T) {
	c := load(t)
	c.Defaults.AttemptTimeoutMS = 300_000
	err := c.Validate(tools.NewRegistry().Names())
	if err == nil || !strings.Contains(err.Error(), "workflows.watchdog_stall_ms") {
		t.Fatalf("a 300 s attempt timeout with a 120 s stall limit must be rejected, got %v", err)
	}
	c.Workflows.WatchdogStallMS = 310_000
	if err := c.Validate(tools.NewRegistry().Names()); err != nil {
		t.Fatalf("raising the stall limit should fix it: %v", err)
	}
}

func TestStoreUpdateIsAtomic(t *testing.T) {
	s := config.NewStore(load(t), tools.NewRegistry().Names())
	_, v1 := s.Get()
	_, _, err := s.Update(func(c *config.Config) error {
		c.Router.EscalationThreshold = 0.9
		c.Defaults.Retry.MaxRetries = 99 // invalid: whole update must be discarded
		return nil
	})
	if err == nil {
		t.Fatal("expected a validation error")
	}
	cur, v2 := s.Get()
	if v2 != v1 || cur.Router.EscalationThreshold != 0.6 {
		t.Fatalf("invalid update leaked: version %d->%d threshold %v", v1, v2, cur.Router.EscalationThreshold)
	}
	if _, v3, err := s.Update(func(c *config.Config) error { c.Router.EscalationThreshold = 0.9; return nil }); err != nil || v3 != v1+1 {
		t.Fatalf("valid update: v=%d err=%v", v3, err)
	}
	if reset, _ := s.Reset(); reset.Router.EscalationThreshold != 0.6 {
		t.Fatal("reset should restore the startup config")
	}
}

func TestRouting(t *testing.T) {
	c := load(t)
	summarizer, _ := c.Agent("summarizer")
	planner, _ := c.Agent("planner")
	writer, _ := c.Agent("reply-writer")

	d, _ := router.Route(c, summarizer, "")
	if d.Tier != "slm" || d.Model.Model != "slm-summarizer" {
		t.Errorf("summarize role -> slm, got %+v", d)
	}
	d, _ = router.Route(c, planner, "")
	if d.Tier != "llm" {
		t.Errorf("plan role -> llm, got %+v", d)
	}
	d, _ = router.Route(c, summarizer, "llm")
	if d.Tier != "llm" || !d.Override {
		t.Errorf("caller override -> llm, got %+v", d)
	}
	summarizer.Tier = "llm"
	d, _ = router.Route(c, summarizer, "")
	if d.Tier != "llm" || !strings.Contains(d.Reason, "pinned") {
		t.Errorf("pinned tier, got %+v", d)
	}
	d, _ = router.Route(c, writer, "llm")
	if d.Tier != "slm" || !strings.Contains(d.Reason, "not configured") {
		t.Errorf("missing tier falls back to the other one, got %+v", d)
	}
	if esc, ok := router.Escalate(summarizer, "guard"); !ok || esc.Model.Model != "llm-summarizer" {
		t.Errorf("escalate: %+v %v", esc, ok)
	}
	if _, ok := router.Escalate(writer, "x"); ok {
		t.Error("an SLM-only agent cannot escalate")
	}
}
