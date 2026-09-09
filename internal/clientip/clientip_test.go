package clientip

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

// TestFromRequest_TrustedProxies is the audit M-15 guard: X-Forwarded-For is
// believed only from a trusted proxy, and only the right-most untrusted hop
// counts, so a client cannot choose its own audit attribution.
func TestFromRequest_TrustedProxies(t *testing.T) {
	t.Cleanup(func() { _ = SetTrustedProxies(nil) })

	// No trusted proxies: the header is ignored outright.
	if err := SetTrustedProxies(nil); err != nil {
		t.Fatal(err)
	}
	if got := FromRequest(req("203.0.113.9:4321", "1.2.3.4")); got != "203.0.113.9" {
		t.Fatalf("untrusted peer with XFF -> %q, want peer", got)
	}

	if err := SetTrustedProxies([]string{"10.0.0.0/8", "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		r    *http.Request
		want string
	}{
		{"peer not trusted, header ignored", req("203.0.113.9:1", "1.2.3.4"), "203.0.113.9"},
		{"trusted peer, single hop", req("10.1.2.3:1", "198.51.100.7"), "198.51.100.7"},
		{"trusted peer, client-forged prefix ignored", req("10.1.2.3:1", "6.6.6.6, 198.51.100.7"), "198.51.100.7"},
		{"trusted peer, chain of trusted proxies", req("10.1.2.3:1", "198.51.100.7, 10.9.9.9"), "198.51.100.7"},
		{"trusted peer, two header lines", req("10.1.2.3:1", "6.6.6.6", "198.51.100.7, 10.9.9.9"), "198.51.100.7"},
		{"trusted peer, all hops trusted", req("10.1.2.3:1", "10.5.5.5"), "10.1.2.3"},
		{"trusted peer, garbage hop skipped", req("10.1.2.3:1", "198.51.100.7, not-an-ip"), "198.51.100.7"},
		{"trusted peer, no header", req("10.1.2.3:1"), "10.1.2.3"},
		{"loopback trusted, ipv6 client", req("127.0.0.1:9", "2001:db8::1"), "2001:db8::1"},
		{"ipv6 peer with port", req("[2001:db8::9]:443"), "2001:db8::9"},
		{"mapped ipv4 hop normalised", req("10.1.2.3:1", "::ffff:198.51.100.7"), "198.51.100.7"},
		{"unparsable peer returned verbatim", req("pipe"), "pipe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromRequest(tc.r); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if err := SetTrustedProxies([]string{"nonsense"}); err == nil {
		t.Fatal("garbage CIDR accepted")
	}
}
