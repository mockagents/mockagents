package tenancy

import "testing"

// TestParseBearerToken pins the F-MW-002 robustness: the scheme is matched
// case-insensitively and surrounding whitespace is tolerated, while a
// near-miss is rejected rather than silently yielding a token.
func TestParseBearerToken(t *testing.T) {
	cases := []struct {
		header    string
		wantToken string
		wantOK    bool
	}{
		{"Bearer abc123", "abc123", true},
		{"bearer abc123", "abc123", true},   // lowercase scheme
		{"BEARER abc123", "abc123", true},   // uppercase scheme
		{"  Bearer   abc123  ", "abc123", true}, // surrounding + inner whitespace
		{"Bearer\tabc123", "abc123", true},  // tab delimiter
		{"", "", false},                     // empty
		{"abc123", "", false},               // no scheme
		{"Bearer", "", false},               // scheme only
		{"Bearer ", "", false},              // scheme + space, no token
		{"Bearertoken", "", false},          // scheme not whitespace-delimited
		{"Basic abc123", "", false},         // wrong scheme
	}
	for _, c := range cases {
		gotToken, gotOK := ParseBearerToken(c.header)
		if gotToken != c.wantToken || gotOK != c.wantOK {
			t.Errorf("ParseBearerToken(%q) = (%q, %v), want (%q, %v)",
				c.header, gotToken, gotOK, c.wantToken, c.wantOK)
		}
	}
}
