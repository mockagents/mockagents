package adapter

import (
	"strings"
	"testing"
)

// BenchmarkGenerateID tracks the PERF-07 id-generation cost for the adapter's
// response/session/tool/message ids.
func BenchmarkGenerateID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = generateID()
	}
}

// TestGenerateID_UniqueAndHex is the PERF-07 guard: ids are non-empty lowercase
// hex and don't repeat across a tight loop (uniqueness is the property that
// matters; predictability is explicitly acceptable here).
func TestGenerateID_UniqueAndHex(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := generateID()
		if id == "" {
			t.Fatal("generateID returned empty")
		}
		if strings.TrimLeft(id, "0123456789abcdef") != "" {
			t.Fatalf("generateID = %q, want lowercase hex", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("generateID produced a duplicate: %q", id)
		}
		seen[id] = struct{}{}
	}
}
