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
