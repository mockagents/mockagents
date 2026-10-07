package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/llm"
)

// Order is a demo order record.
type Order struct {
	ID        string  `json:"id"`
	Customer  string  `json:"customer"`
	Item      string  `json:"item"`
	Amount    float64 `json:"amount"`
	Status    string  `json:"status"`
	Charges   int     `json:"charges"`
	Refunded  bool    `json:"refunded"`
	RefundRef string  `json:"refund_ref,omitempty"`
}

// orderBook is the shared, mutable order store behind lookup_order and
// issue_refund.
type orderBook struct {
	mu     sync.Mutex
	orders map[string]*Order
	seq    int
}

func newOrderBook() *orderBook {
	return &orderBook{orders: map[string]*Order{
		"ORD-1001": {ID: "ORD-1001", Customer: "Dana Whitfield", Item: "Pro plan (monthly)", Amount: 49.99, Status: "active", Charges: 2},
		"ORD-1002": {ID: "ORD-1002", Customer: "Sam Okafor", Item: "Team plan (annual)", Amount: 480.00, Status: "active", Charges: 1},
		"ORD-1003": {ID: "ORD-1003", Customer: "Lee Marsh", Item: "Starter plan", Amount: 9.99, Status: "cancelled", Charges: 1},
	}}
}

type lookupOrder struct{ orders *orderBook }

func (lookupOrder) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "lookup_order",
		Description: "Fetch an order by id.",
		Parameters: schema([]string{"order_id"}, map[string]any{
			"order_id": map[string]any{"type": "string", "description": "Order id, for example ORD-1001."},
		}),
	}
}

func (lookupOrder) RequiresApproval() bool { return false }

func (t lookupOrder) Execute(_ context.Context, args map[string]any) (any, error) {
	id := str(args, "order_id")
	t.orders.mu.Lock()
	defer t.orders.mu.Unlock()
	o, ok := t.orders.orders[id]
	if !ok {
		return nil, fmt.Errorf("order %q: %w", id, ErrNotFound)
	}
	c := *o
	return c, nil
}

// issueRefund is side-effecting, so it requires human approval and is
// idempotent per order: a second refund for the same order returns the
// original refund reference instead of paying twice. Retries and agent
// loops make duplicate tool calls a real risk.
type issueRefund struct{ orders *orderBook }

func (issueRefund) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "issue_refund",
		Description: "Refund an order to the original payment method. Requires human approval.",
		Parameters: schema([]string{"order_id", "amount", "reason"}, map[string]any{
			"order_id": map[string]any{"type": "string"},
			"amount":   map[string]any{"type": "number"},
			"reason":   map[string]any{"type": "string"},
		}),
	}
}

func (issueRefund) RequiresApproval() bool { return true }

func (t issueRefund) Execute(_ context.Context, args map[string]any) (any, error) {
	id := str(args, "order_id")
	amount, _ := args["amount"].(float64)
	t.orders.mu.Lock()
	defer t.orders.mu.Unlock()
	o, ok := t.orders.orders[id]
	if !ok {
		return nil, fmt.Errorf("order %q: %w", id, ErrNotFound)
	}
	if amount <= 0 || amount > o.Amount {
		return nil, fmt.Errorf("refund amount %.2f is outside 0 < amount <= %.2f", amount, o.Amount)
	}
	if o.Refunded {
		return map[string]any{"status": "already_refunded", "order_id": id, "refund_ref": o.RefundRef, "idempotent": true}, nil
	}
	t.orders.seq++
	o.Refunded = true
	o.RefundRef = fmt.Sprintf("RF-%s-%d", time.Now().UTC().Format("20060102"), t.orders.seq)
	return map[string]any{"status": "refunded", "order_id": id, "amount": amount, "refund_ref": o.RefundRef}, nil
}
