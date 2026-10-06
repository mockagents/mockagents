package storage

import (
	"math/rand/v2"
	"strings"
	"testing"
)

const fuzzAlnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// fuzzSecretValue derives a deterministic 40-char alphanumeric credential
// body from the fuzzer-chosen seed, so a failing input replays exactly.
func fuzzSecretValue(seed uint64) string {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	var b strings.Builder
	for range 40 {
		b.WriteByte(fuzzAlnum[rng.IntN(len(fuzzAlnum))])
	}
	return b.String()
}

// FuzzSanitizeBody exercises the log/cassette key masker with an
// API-key-shaped token injected between two arbitrary strings.
//
// Invariants:
//   - SanitizeBody never panics;
//   - it is idempotent (documented contract): SanitizeBody(SanitizeBody(x))
//     == SanitizeBody(x);
//   - the injected credential never survives: for sk-/key- shaped keys the
//     secret value after the prefix is gone, and a "Bearer <token>" never
//     appears verbatim in the output.
func FuzzSanitizeBody(f *testing.F) {
	seeds := []struct {
		prefix, suffix string
		kind           uint8
	}{
		{`{"model":"gpt-4o","api_key":"`, `","messages":[]}`, 0},
		{`{"headers":{"x-api-key":"`, `"}}`, 1},
		{"Authorization: ", "\r\nContent-Type: application/json", 3},
		{`{"a":"sk-***","b":"`, `"}`, 0},
		{`{"key":"sk-***`, `"}`, 2},
		{"", "", 4},
		{"Bearer ", ",", 0},
		{"sk-", " tail", 1},
		{"key-", "\n", 2},
		{"\xff\xfe", "\x00", 3},
	}
	for i, s := range seeds {
		f.Add(s.prefix, s.suffix, s.kind, uint64(i))
	}
	f.Fuzz(func(t *testing.T, prefix, suffix string, kind uint8, seed uint64) {
		value := fuzzSecretValue(seed)
		var token string
		checkValue := true
		switch kind % 5 {
		case 0:
			token = "sk-" + value
		case 1:
			token = "sk-ant-api03-" + value
		case 2:
			token = "key-" + value
		case 3:
			token = "sk-proj-" + value
		case 4:
			token = "Bearer " + value
			checkValue = false
		}
		// The fuzzer could in principle reproduce the random value in the
		// surrounding text; that occurrence is not the injected secret.
		if strings.Contains(prefix, value) || strings.Contains(suffix, value) {
			t.Skip()
		}
		in := prefix + token + suffix
		out := SanitizeBody(in)

		if again := SanitizeBody(out); again != out {
			t.Fatalf("not idempotent:\nin:    %q\nonce:  %q\ntwice: %q", in, out, again)
		}
		if strings.Contains(out, token) {
			t.Fatalf("credential survived verbatim:\nin:  %q\nout: %q", in, out)
		}
		if checkValue && strings.Contains(out, value) {
			t.Fatalf("credential value survived:\nin:  %q\nout: %q", in, out)
		}
	})
}
