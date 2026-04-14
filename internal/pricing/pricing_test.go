package pricing

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// almostEqual compares floats with a tight epsilon. Cost math stays
// in the cents-and-below range where double precision is comfortable,
// so we don't need a fancy comparison helper.
func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// --- Price math ---

func TestPriceEstimate(t *testing.T) {
	p := Price{PromptPer1KUSD: 0.003, CompletionPer1KUSD: 0.015}
	// 2000 prompt + 500 completion
	//   = 2 * 0.003 + 0.5 * 0.015
	//   = 0.006 + 0.0075
	//   = 0.0135
	got := p.Estimate(2000, 500)
	if !almostEqual(got, 0.0135) {
		t.Errorf("Estimate(2000, 500) = %v, want 0.0135", got)
	}
}

func TestPriceEstimateClampsNegative(t *testing.T) {
	p := Price{PromptPer1KUSD: 0.001, CompletionPer1KUSD: 0.001}
	if got := p.Estimate(-100, -50); got != 0 {
		t.Errorf("negative inputs should clamp to zero, got %v", got)
	}
}

// --- Default table ---

func TestDefaultTableHasCommonModels(t *testing.T) {
	tbl := NewDefaultTable()
	for _, model := range []string{
		"gpt-4o", "gpt-4o-mini", "claude-3-5-sonnet-latest",
		"claude-3-haiku-20240307",
	} {
		if _, ok := tbl.Lookup(model); !ok {
			t.Errorf("missing default price for %q", model)
		}
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	tbl := NewDefaultTable()
	lo, okLo := tbl.Lookup("gpt-4o")
	up, okUp := tbl.Lookup("GPT-4O")
	if !okLo || !okUp {
		t.Fatal("expected case-insensitive hit")
	}
	if lo.PromptPer1KUSD != up.PromptPer1KUSD {
		t.Errorf("lookup case mismatch: %v vs %v", lo, up)
	}
}

func TestLookupUnknownReturnsFallback(t *testing.T) {
	tbl := NewDefaultTable()
	p, ok := tbl.Lookup("nonexistent-model-2099")
	if ok {
		t.Error("unknown model should report ok=false")
	}
	if p.PromptPer1KUSD != 0 || p.CompletionPer1KUSD != 0 {
		t.Errorf("fallback should be zero-cost, got %v", p)
	}
}

func TestTableEstimate(t *testing.T) {
	tbl := NewDefaultTable()
	// gpt-4o priced at 0.0025 prompt / 0.010 completion per 1K.
	// 10000 + 2000 tokens =>
	//   10 * 0.0025 + 2 * 0.010 = 0.025 + 0.020 = 0.045
	got := tbl.Estimate("gpt-4o", 10000, 2000)
	if !almostEqual(got, 0.045) {
		t.Errorf("gpt-4o(10k,2k) = %v, want 0.045", got)
	}
}

// --- YAML override ---

func TestLoadYAMLOverridesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prices.yaml")
	body := `
prices:
  - model: gpt-4o
    prompt_per_1k_usd: 0.010
    completion_per_1k_usd: 0.040
  - model: custom-model
    prompt_per_1k_usd: 0.001
    completion_per_1k_usd: 0.002
fallback:
  model: (unknown)
  prompt_per_1k_usd: 0.0001
  completion_per_1k_usd: 0.0001
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl := NewDefaultTable()
	if err := tbl.LoadYAML(path); err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}

	gpt4o, _ := tbl.Lookup("gpt-4o")
	if !almostEqual(gpt4o.PromptPer1KUSD, 0.010) {
		t.Errorf("gpt-4o override not applied: %v", gpt4o)
	}

	custom, ok := tbl.Lookup("custom-model")
	if !ok || !almostEqual(custom.PromptPer1KUSD, 0.001) {
		t.Errorf("custom-model not added: %v ok=%v", custom, ok)
	}

	// Fallback override — an unknown model now gets a nonzero price.
	unknown, ok := tbl.Lookup("nowhere")
	if ok {
		t.Error("unknown model should still report ok=false")
	}
	if !almostEqual(unknown.PromptPer1KUSD, 0.0001) {
		t.Errorf("fallback override not applied: %v", unknown)
	}
}

func TestFromEnvDefaultWhenUnset(t *testing.T) {
	t.Setenv("MOCKAGENTS_PRICING", "")
	tbl, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if _, ok := tbl.Lookup("gpt-4o"); !ok {
		t.Error("default table should contain gpt-4o")
	}
}

func TestFromEnvFileNotFoundReturnsError(t *testing.T) {
	t.Setenv("MOCKAGENTS_PRICING", filepath.Join(t.TempDir(), "missing.yaml"))
	tbl, err := FromEnv()
	if err == nil {
		t.Error("expected error for missing file")
	}
	// Table is still usable with defaults.
	if _, ok := tbl.Lookup("gpt-4o"); !ok {
		t.Error("table should fall back to defaults on load error")
	}
}

// --- Usage extraction ---

func TestExtractUsageOpenAI(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"choices": [{"message": {"content": "hi"}}],
		"usage": {"prompt_tokens": 12, "completion_tokens": 34, "total_tokens": 46}
	}`)
	u := ExtractUsage(body)
	if u.PromptTokens != 12 || u.CompletionTokens != 34 {
		t.Errorf("unexpected usage: %+v", u)
	}
	if u.Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", u.Model)
	}
	if u.Total() != 46 {
		t.Errorf("Total() = %d, want 46", u.Total())
	}
}

func TestExtractUsageAnthropic(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"content": [{"type": "text", "text": "hi"}],
		"usage": {"input_tokens": 7, "output_tokens": 18}
	}`)
	u := ExtractUsage(body)
	if u.PromptTokens != 7 || u.CompletionTokens != 18 {
		t.Errorf("unexpected usage: %+v", u)
	}
	if u.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("model = %q", u.Model)
	}
}

func TestExtractUsageEmptyAndInvalid(t *testing.T) {
	if u := ExtractUsage(nil); u.Total() != 0 {
		t.Errorf("nil body should return zero Usage, got %+v", u)
	}
	if u := ExtractUsage([]byte(``)); u.Total() != 0 {
		t.Errorf("empty body should return zero Usage, got %+v", u)
	}
	if u := ExtractUsage([]byte(`not json`)); u.Total() != 0 {
		t.Errorf("garbage body should return zero Usage, got %+v", u)
	}
	// Missing usage block.
	if u := ExtractUsage([]byte(`{"model":"x"}`)); u.Total() != 0 {
		t.Errorf("missing usage should return zero Usage, got %+v", u)
	}
}
