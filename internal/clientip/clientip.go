// Package clientip resolves the client address of a request in a way that
// cannot be forged by the client itself. X-Forwarded-For is honoured only
// when the direct peer is a configured trusted proxy, and then only the
// right-most hop that is not itself a trusted proxy is used — the hops a
// client prepends are ignored. Before this the audit log recorded whatever
// X-Forwarded-For said (readiness audit M-15), so a credential-stuffing
// source could attribute its attempts to any address it liked.
//
// It is a leaf package so both the tenancy middleware (rate limiting by
// source) and the audit recorder (attribution) resolve the same address.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
)

// trusted holds the proxy CIDRs. nil means "trust nobody": every request is
// attributed to its direct peer.
var trusted atomic.Pointer[[]netip.Prefix]

// SetTrustedProxies installs the CIDRs (or single addresses) whose
// X-Forwarded-For header is believed. An empty list clears it.
func SetTrustedProxies(cidrs []string) error {
	var prefixes []netip.Prefix
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if p, err := netip.ParsePrefix(c); err == nil {
			prefixes = append(prefixes, p)
			continue
		}
		a, err := netip.ParseAddr(c)
		if err != nil {
			return fmt.Errorf("trusted proxy %q is neither a CIDR nor an address", c)
		}
		prefixes = append(prefixes, netip.PrefixFrom(a, a.BitLen()))
	}
	if len(prefixes) == 0 {
		trusted.Store(nil)
		return nil
	}
	trusted.Store(&prefixes)
	return nil
}

// TrustedProxies returns the configured CIDRs (for logging / diagnostics).
func TrustedProxies() []netip.Prefix {
	if p := trusted.Load(); p != nil {
		return append([]netip.Prefix(nil), (*p)...)
	}
	return nil
}

func isTrusted(ip netip.Addr) bool {
	p := trusted.Load()
	if p == nil || !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	for _, prefix := range *p {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// hostOf strips a port from "host:port" / "[v6]:port"; a bare host is
// returned as-is.
func hostOf(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return strings.Trim(addr, "[]")
}

// FromRequest returns the client's IP as a string. The direct peer is used
// unless it is a trusted proxy, in which case X-Forwarded-For is walked from
// the right and the first hop that is not a trusted proxy wins. If every hop
// is trusted (or the header is absent/garbage) the direct peer is returned.
// Unparsable peer addresses are returned verbatim so nothing is lost from
// the audit trail.
func FromRequest(r *http.Request) string {
	peer := hostOf(r.RemoteAddr)
	peerAddr, err := netip.ParseAddr(peer)
	if err != nil || !isTrusted(peerAddr) {
		return peer
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, h := range strings.Split(v, ",") {
			if h = strings.TrimSpace(h); h != "" {
				hops = append(hops, h)
			}
		}
	}
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(hostOf(hops[i]))
		if err != nil {
			continue // a garbage hop cannot be trusted or attributed; skip it
		}
		if !isTrusted(a) {
			return a.Unmap().String()
		}
	}
	return peer
}
