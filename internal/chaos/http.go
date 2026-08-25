package chaos

import "net/http"

// DisconnectHTTP closes a hijackable HTTP/1.x connection without writing a
// response. It returns false for transports such as HTTP/2 and test recorders,
// allowing callers to emit a deterministic 502 fallback.
func DisconnectHTTP(w http.ResponseWriter) bool {
	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ResetHTTP aborts a hijackable HTTP/1.x TCP connection. Setting linger to
// zero asks the kernel to send RST instead of a graceful FIN before closing.
// Wrapped connections still receive an immediate close. It returns false when
// the response transport cannot be hijacked so callers can emit a fallback.
func ResetHTTP(w http.ResponseWriter) bool {
	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return false
	}
	if linger, ok := conn.(interface{ SetLinger(int) error }); ok {
		_ = linger.SetLinger(0)
	}
	_ = conn.Close()
	return true
}
