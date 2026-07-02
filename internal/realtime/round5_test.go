// Round-5 fidelity regression tests (2026-07-02 eval): phantom items on early
// cancel, chain repair on delete, out-of-band item scoping, session.update
// mid-speech, voice locking, and assorted wire-shape corrections.
package realtime

import (
	"context"
	"testing"
)

func mkUserItem(id, text string) *ClientEvent {
	return &ClientEvent{Type: "conversation.item.create",
		Item: []byte(`{"id":"` + id + `","type":"message","role":"user","content":[{"type":"input_text","text":"` + text + `"}]}`)}
}

// T-F3: deleting the chain-tail item must repair lastItemID — the next item
// chains off the new tail, not off an id the server itself would reject.
func TestDeleteTailRepairsChain(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sd", "", fakeGen("ok"))
	s.Handle(ctx, mkUserItem("item_a", "one"))
	s.Handle(ctx, mkUserItem("item_b", "two"))

	if evs := s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: "item_b"}); evs[0]["type"] != "conversation.item.deleted" {
		t.Fatalf("delete = %v", typesOf(evs))
	}
	added := firstEvent(s.Handle(ctx, mkUserItem("item_c", "three")), "conversation.item.added")
	if added["previous_item_id"] != "item_a" {
		t.Errorf("after tail delete, previous_item_id = %v, want item_a", added["previous_item_id"])
	}

	// Deleting everything empties the chain: the next item is first (prev null).
	s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: "item_a"})
	s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: "item_c"})
	added = firstEvent(s.Handle(ctx, mkUserItem("item_d", "four")), "conversation.item.added")
	if added["previous_item_id"] != nil {
		t.Errorf("after deleting all items, previous_item_id = %v, want null", added["previous_item_id"])
	}
}

// T-F6: an out-of-band response's items belong to no conversation — they are
// listed in ITS response.done output but must not be retrievable or anchor the
// conversation chain.
func TestOutOfBandItemsNotRetrievable(t *testing.T) {
	ctx := context.Background()
	s := NewSession("so", "", fakeGen("side answer"))
	s.Handle(ctx, mkUserItem("item_u", "main topic"))

	evs := s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none"}`)})
	output := firstEvent(evs, "response.done")["response"].(map[string]any)["output"].([]any)
	if len(output) == 0 {
		t.Fatal("OOB response must still list its output items")
	}
	oobID := output[0].(map[string]any)["id"].(string)

	got := s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve", ItemID: oobID})
	if got[0]["type"] != "error" || got[0]["error"].(map[string]any)["code"] != "item_not_found" {
		t.Errorf("OOB item retrieve = %v, want item_not_found (it joined no conversation)", got[0])
	}
	// The chain tail is untouched by the OOB response.
	added := firstEvent(s.Handle(ctx, mkUserItem("item_v", "next")), "conversation.item.added")
	if added["previous_item_id"] != "item_u" {
		t.Errorf("after OOB response, previous_item_id = %v, want item_u", added["previous_item_id"])
	}
}
