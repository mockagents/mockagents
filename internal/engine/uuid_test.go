package engine

import (
	"fmt"
	"regexp"
	"testing"
)

// uuidV4Re matches the canonical 8-4-4-4-12 layout with the version nibble
// pinned to 4 and the variant nibble to 8/9/a/b.
var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestGenerateUUID_V4Format is the PERF-17 guard: the hand-rolled hex layout
// must still produce a well-formed, unique RFC 4122 v4 UUID (the change replaced
// fmt.Sprintf, not the crypto/rand source or the version/variant bits).
func TestGenerateUUID_V4Format(t *testing.T) {
	const n = 5000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := generateUUID()
		if len(id) != 36 {
			t.Fatalf("len(%q) = %d, want 36", id, len(id))
		}
		if !uuidV4Re.MatchString(id) {
			t.Fatalf("generateUUID = %q, not a v4 UUID", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate UUID: %q", id)
		}
		seen[id] = struct{}{}
	}
}

// BenchmarkGenerateUUID_New measures the hand-rolled hex path.
func BenchmarkGenerateUUID_New(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = generateUUID()
	}
}

// BenchmarkGenerateUUID_Old reproduces the pre-PERF-17 fmt.Sprintf("%08x-...")
// formatting for an honest comparison (same crypto/rand draw + bit-twiddling).
func BenchmarkGenerateUUID_Old(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var by [16]byte
		// Deterministic fill so the benchmark measures formatting, not rand.
		for j := range by {
			by[j] = byte(i + j)
		}
		by[6] = (by[6] & 0x0f) | 0x40
		by[8] = (by[8] & 0x3f) | 0x80
		_ = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			by[0:4], by[4:6], by[6:8], by[8:10], by[10:16])
	}
}
