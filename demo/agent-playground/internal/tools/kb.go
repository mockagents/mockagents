package tools

import (
	"context"
	"sort"
	"strings"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/llm"
)

// Article is one knowledge-base entry. The research-brief workflow's
// grounding guard checks summaries against the text of articles returned by
// search_kb, so every figure an agent may legitimately cite lives here.
type Article struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Tags  []string `json:"tags"`
}

// KnowledgeBase is the static corpus behind search_kb.
var KnowledgeBase = []Article{
	{ID: "KB-100", Title: "Deterministic mock testing", Tags: []string{"mock", "testing", "deterministic", "scenario"},
		Body: "MockAgents replays scripted scenarios, so a test suite of 120 agent calls runs in under 2 seconds with zero token spend. Scenario matching is case-insensitive and the first matching rule wins."},
	{ID: "KB-101", Title: "Retry with exponential backoff", Tags: []string{"retry", "backoff", "429", "503", "resilience"},
		Body: "Retry transient failures (HTTP 429, 500, 502, 503, 504 and connection resets) with exponential backoff. A 200 ms base delay doubles on each attempt: 200 ms, 400 ms, 800 ms, 1600 ms, 3200 ms. Cap retries at 5 and add up to 20% jitter. Always honour the Retry-After header on 429 responses."},
	{ID: "KB-102", Title: "Fallback models", Tags: []string{"fallback", "retry", "resilience", "degraded"},
		Body: "When retries are exhausted, fall back to a backup model on a different provider instead of retrying forever. A fallback answer should be labelled so reviewers know it came from the backup tier."},
	{ID: "KB-103", Title: "SLM and LLM routing", Tags: []string{"slm", "llm", "routing", "cost", "tier"},
		Body: "Small language models handle summarization, classification and extraction at roughly 0.6 USD per million output tokens; large models cost about 10 USD per million output tokens and are reserved for planning, reasoning and judging. Escalate from the SLM to the LLM when classifier confidence falls below 0.6."},
	{ID: "KB-104", Title: "Grounding and hallucination guards", Tags: []string{"grounding", "hallucination", "guard", "verification"},
		Body: "A grounding guard checks that every figure and citation in a summary appears in the retrieved evidence. Ungrounded output is regenerated once with a strict-grounding instruction on the LLM tier, then routed to a human reviewer."},
	{ID: "KB-105", Title: "Troubleshooting 504 timeouts", Tags: []string{"timeout", "504", "error", "troubleshooting", "crash"},
		Body: "A 504 Gateway Timeout from the API means an upstream call exceeded 30 seconds. Lower the per-attempt timeout to 15 seconds, retry with backoff, and check the status page. Persistent timeouts after 3 retries should be escalated to on-call."},
	{ID: "KB-106", Title: "Human review checkpoints", Tags: []string{"review", "human", "approval", "revision"},
		Body: "Every AI-generated output is recorded for audit, and final outputs pass a blocking review gate. Reviewers can approve, reject or request a revision; each run allows at most 2 revisions."},
	{ID: "KB-107", Title: "Refund policy", Tags: []string{"refund", "billing", "charge", "policy"},
		Body: "Duplicate charges are refunded in full to the original payment method within 5 business days. Refunds above 100 USD require a supervisor; every refund issued by an agent requires human approval."},
}

// KnownCitations returns the set of valid article ids.
func KnownCitations() map[string]bool {
	out := make(map[string]bool, len(KnowledgeBase))
	for _, a := range KnowledgeBase {
		out[a.ID] = true
	}
	return out
}

type searchKB struct{}

func (searchKB) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "search_kb",
		Description: "Search the internal knowledge base and return the best matching articles.",
		Parameters: schema([]string{"query"}, map[string]any{
			"query": map[string]any{"type": "string", "description": "Free-text search query."},
		}),
	}
}

func (searchKB) RequiresApproval() bool { return false }

// Execute scores articles by keyword overlap (tags count double) and returns
// the top 3.
func (searchKB) Execute(_ context.Context, args map[string]any) (any, error) {
	query := strings.ToLower(str(args, "query"))
	terms := strings.FieldsFunc(query, func(r rune) bool { return r == ' ' || r == ',' || r == '/' || r == '-' })
	type hit struct {
		a     Article
		score int
	}
	var hits []hit
	for _, a := range KnowledgeBase {
		hay := strings.ToLower(a.Title + " " + a.Body)
		score := 0
		for _, t := range terms {
			if len(t) < 2 {
				continue
			}
			if strings.Contains(hay, t) {
				score++
			}
			for _, tag := range a.Tags {
				if tag == t {
					score += 2
				}
			}
		}
		if score > 0 {
			hits = append(hits, hit{a, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].a.ID < hits[j].a.ID
	})
	if len(hits) > 3 {
		hits = hits[:3]
	}
	results := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		results = append(results, map[string]any{"id": h.a.ID, "title": h.a.Title, "body": h.a.Body, "score": h.score})
	}
	return map[string]any{"query": query, "results": results}, nil
}
