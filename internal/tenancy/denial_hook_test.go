package tenancy

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// TestDenialHook_LastWriterWinsAndClear exercises SetDenialHook/fireDenial
// directly: the most recently installed hook wins, and nil disables it
// (F-SV-007 semantics).
func TestDenialHook_LastWriterWinsAndClear(t *testing.T) {
	t.Cleanup(func() { SetDenialHook(nil) })
	req := httptest.NewRequest("GET", "/x", nil)

	var calls1, calls2 int
	SetDenialHook(func(_ *http.Request, _ int, _ string) { calls1++ })
	fireDenial(req, 401, "r")
	if calls1 != 1 {
		t.Fatalf("hook1 calls = %d, want 1", calls1)
	}

	// Installing a new hook replaces the old (last-writer-wins).
	SetDenialHook(func(_ *http.Request, _ int, _ string) { calls2++ })
	fireDenial(req, 403, "r")
	if calls1 != 1 {
		t.Errorf("old hook fired after replacement: %d", calls1)
	}
	if calls2 != 1 {
		t.Errorf("hook2 calls = %d, want 1", calls2)
	}

	// nil disables — fireDenial must be a no-op, no panic.
	SetDenialHook(nil)
	fireDenial(req, 401, "r")
	if calls1 != 1 || calls2 != 1 {
		t.Errorf("a hook fired after clear: %d %d", calls1, calls2)
	}
}

// TestDenialHook_ConcurrentSetAndFire is the F-SV-007 race-safety smoke test:
// SetDenialHook and fireDenial run concurrently without panicking. The
// codebase can't run `go test -race` (no cgo), so this is a liveness/no-panic
// smoke — the atomic.Pointer is what actually makes the access safe — but it
// would still crash on a plainly broken implementation (e.g. calling a hook
// pointer freed mid-swap).
func TestDenialHook_ConcurrentSetAndFire(t *testing.T) {
	t.Cleanup(func() { SetDenialHook(nil) })
	req := httptest.NewRequest("GET", "/x", nil)
	var fired atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if n%2 == 0 {
					SetDenialHook(func(_ *http.Request, _ int, _ string) { fired.Add(1) })
				} else {
					fireDenial(req, 401, "r")
				}
			}
		}(i)
	}
	wg.Wait()
	// No assertion on fired (racy by design): reaching here without a panic
	// is the pass condition.
}
