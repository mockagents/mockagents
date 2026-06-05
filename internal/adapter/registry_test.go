package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routePatterns flattens a registry's adapters into the set of route
// patterns they mount, for order-independent assertions.
func routePatterns(reg *Registry) map[string]bool {
	patterns := map[string]bool{}
	for _, a := range reg.Adapters() {
		for _, route := range a.Routes() {
			patterns[route.Pattern] = true
		}
	}
	return patterns
}

func TestDefaultRegistry(t *testing.T) {
	reg := DefaultRegistry(testEngine())

	names := make([]string, 0, len(reg.Adapters()))
	for _, a := range reg.Adapters() {
		names = append(names, a.Name())
	}
	assert.Equal(t, []string{"openai", "anthropic"}, names, "default adapters mount in order")

	got := routePatterns(reg)
	for _, want := range []string{
		"POST /v1/chat/completions",
		"GET /v1/models",
		"POST /v1/messages",
	} {
		assert.Truef(t, got[want], "default registry should serve %q", want)
	}

	// Every route must carry a non-nil handler — a nil here would panic
	// at mux.HandleFunc mount time.
	for _, a := range reg.Adapters() {
		for _, route := range a.Routes() {
			assert.NotNilf(t, route.Handler, "%s route %q has a nil handler", a.Name(), route.Pattern)
		}
	}
}

// fakeAdapter stands in for a future third provider, proving a new
// adapter mounts through the registry boundary with no changes to the
// server's route wiring (REF-05).
type fakeAdapter struct{ hit *bool }

func (f *fakeAdapter) Name() string { return "fake" }
func (f *fakeAdapter) Routes() []Route {
	return []Route{{Pattern: "POST /v1/fake", Handler: func(w http.ResponseWriter, r *http.Request) {
		*f.hit = true
		w.WriteHeader(http.StatusTeapot)
	}}}
}

func TestRegistry_RegisterAndMountCustomAdapter(t *testing.T) {
	var hit bool
	reg := NewRegistry()
	reg.Register(&fakeAdapter{hit: &hit})

	require.Len(t, reg.Adapters(), 1)
	assert.Equal(t, "fake", reg.Adapters()[0].Name())

	// Mount exactly the way the server does and confirm the route is live.
	mux := http.NewServeMux()
	for _, a := range reg.Adapters() {
		for _, route := range a.Routes() {
			mux.HandleFunc(route.Pattern, route.Handler)
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/fake", nil))
	assert.True(t, hit, "custom adapter route was not invoked")
	assert.Equal(t, http.StatusTeapot, rec.Code)
}
