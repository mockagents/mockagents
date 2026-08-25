package chaos

import (
	"net/http/httptest"
	"testing"
)

func TestDisconnectHTTPNonHijackable(t *testing.T) {
	if DisconnectHTTP(httptest.NewRecorder()) {
		t.Fatal("recorder unexpectedly supported disconnect")
	}
}
