package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEvaluate(t *testing.T) {
	cases := map[string]string{
		"200 * 2^4":      "3200",
		"0.6 / 10":       "0.06",
		"(1 + 2) * 3":    "9",
		"2^3^2":          "512", // right-associative
		"-4 + 10":        "6",
		" 7 - 2 - 1 ":    "4",
		"1.5 * (2 - -2)": "6",
	}
	for expr, want := range cases {
		v, err := Evaluate(expr)
		if err != nil || FormatNumber(v) != want {
			t.Errorf("%q = %v (%v), want %s", expr, FormatNumber(v), err, want)
		}
	}
	for _, bad := range []string{"", "1/0", "2 +", "(1", "rm -rf /", "1 2", strings.Repeat("(", 40) + "1" + strings.Repeat(")", 40)} {
		if _, err := Evaluate(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestParseArgs(t *testing.T) {
	r := NewRegistry()
	refund, _ := r.Get("issue_refund")
	if _, err := ParseArgs(refund, `{"order_id": "ORD-1001"`); err == nil {
		t.Error("malformed JSON must be rejected")
	}
	if _, err := ParseArgs(refund, `{"order_id":"ORD-1001","amount":1}`); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Errorf("missing required argument: %v", err)
	}
	if _, err := ParseArgs(refund, `{"order_id":"ORD-1001","amount":"lots","reason":"x"}`); err == nil {
		t.Error("type mismatch must be rejected")
	}
	var ae *ArgumentError
	_, err := ParseArgs(refund, `[]`)
	if !errors.As(err, &ae) {
		t.Errorf("non-object arguments: %v", err)
	}
	if args, err := ParseArgs(refund, `{"order_id":"ORD-1001","amount":1,"reason":"x","extra":true}`); err != nil || args["extra"] != true {
		t.Errorf("extra arguments are tolerated: %v", err)
	}
}

func TestSearchKB(t *testing.T) {
	kb, _ := NewRegistry().Get("search_kb")
	for query, want := range map[string]string{
		"retry backoff fallback":         "KB-101",
		"slm llm routing cost":           "KB-103",
		"grounding hallucination review": "KB-104",
		"timeout 504 troubleshooting":    "KB-105",
	} {
		res, err := kb.Execute(context.Background(), map[string]any{"query": query})
		if err != nil {
			t.Fatal(err)
		}
		results := res.(map[string]any)["results"].([]map[string]any)
		if len(results) == 0 || results[0]["id"] != want {
			t.Errorf("%q: top hit %v, want %s", query, results, want)
		}
	}
}

func TestRefundIsApprovalGatedAndIdempotent(t *testing.T) {
	r := NewRegistry()
	refund, _ := r.Get("issue_refund")
	if !refund.RequiresApproval() {
		t.Fatal("issue_refund must require approval")
	}
	ctx := context.Background()
	first, err := refund.Execute(ctx, map[string]any{"order_id": "ORD-1001", "amount": 49.99, "reason": "dup"})
	if err != nil || first.(map[string]any)["status"] != "refunded" {
		t.Fatalf("first refund: %v %v", first, err)
	}
	second, err := refund.Execute(ctx, map[string]any{"order_id": "ORD-1001", "amount": 49.99, "reason": "dup"})
	if err != nil || second.(map[string]any)["status"] != "already_refunded" || second.(map[string]any)["refund_ref"] != first.(map[string]any)["refund_ref"] {
		t.Fatalf("second refund must be idempotent: %v %v", second, err)
	}
	if _, err := refund.Execute(ctx, map[string]any{"order_id": "ORD-1002", "amount": 9999.0, "reason": "x"}); err == nil {
		t.Error("refund above the order amount must fail")
	}
	lookup, _ := r.Get("lookup_order")
	if _, err := lookup.Execute(ctx, map[string]any{"order_id": "ORD-404"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown order: %v", err)
	}
	// Fresh registries get fresh state.
	refund2, _ := NewRegistry().Get("issue_refund")
	if res, _ := refund2.Execute(ctx, map[string]any{"order_id": "ORD-1001", "amount": 1.0, "reason": "x"}); res.(map[string]any)["status"] != "refunded" {
		t.Error("registries must not share order state")
	}
}
