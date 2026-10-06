package oidcauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// The tests in this file drive the real provider-backed Authenticator (New,
// AuthCodeURL, Exchange) against a hermetic fake identity provider: an
// httptest.Server on 127.0.0.1 that serves discovery, a JWKS, and a token
// endpoint returning ID tokens signed with an RSA key generated in-process.

const (
	testClientID     = "mockagents-gui"
	testClientSecret = "test-client-secret"
	testRedirectURL  = "http://127.0.0.1/auth/callback"
	testKID          = "test-key-1"
)

var (
	keysOnce          sync.Once
	signingKey, rogue *rsa.PrivateKey
)

// testKeys lazily generates the IdP's signing key and an unrelated "rogue" key
// once per test binary (RSA keygen is the slowest part of these tests).
func testKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keysOnce.Do(func() {
		var err error
		if signingKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if rogue, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return signingKey, rogue
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// fakeIdP is a minimal OpenID provider. The token endpoint's behaviour is
// configured per test through tokenHandler; every token request's form is
// recorded so tests can assert what the relying party sent (PKCE verifier,
// code, redirect_uri).
type fakeIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu            sync.Mutex
	tokenRequests []url.Values
	// issuerOverride, when set, is advertised in the discovery document
	// instead of the server's real URL.
	issuerOverride string
	// tokenHandler writes the token endpoint response. Nil = success with an
	// ID token built from claims().
	tokenHandler func(w http.ResponseWriter, f *fakeIdP)
	// idClaims returns the ID token claims to sign. Nil = validClaims.
	idClaims func(f *fakeIdP) map[string]any
	// signWith overrides the signing key (bad-signature cases).
	signWith *rsa.PrivateKey
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, _ := testKeys(t)
	f := &fakeIdP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("GET /jwks", f.jwks)
	mux.HandleFunc("POST /token", f.token)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) issuer() string { return f.srv.URL }

func (f *fakeIdP) discovery(w http.ResponseWriter, _ *http.Request) {
	iss := f.srv.URL
	f.mu.Lock()
	if f.issuerOverride != "" {
		iss = f.issuerOverride
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                f.srv.URL + "/authorize",
		"token_endpoint":                        f.srv.URL + "/token",
		"jwks_uri":                              f.srv.URL + "/jwks",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
	})
}

func (f *fakeIdP) jwks(w http.ResponseWriter, _ *http.Request) {
	pub := f.key.PublicKey
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": testKID,
			"use": "sig",
			"alg": "RS256",
			"n":   b64(pub.N.Bytes()),
			"e":   b64(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
}

func (f *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.tokenRequests = append(f.tokenRequests, r.PostForm)
	handler := f.tokenHandler
	f.mu.Unlock()
	if handler != nil {
		handler(w, f)
		return
	}
	writeTokenResponse(w, f.signedIDToken())
}

func (f *fakeIdP) validClaims() map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":            f.issuer(),
		"aud":            testClientID,
		"sub":            "user-123",
		"email":          "alice@acme.test",
		"email_verified": true,
		"iat":            now.Add(-time.Minute).Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	}
}

func (f *fakeIdP) signedIDToken() string {
	claims := f.validClaims()
	if f.idClaims != nil {
		claims = f.idClaims(f)
	}
	key := f.key
	if f.signWith != nil {
		key = f.signWith
	}
	return mustSign(key, claims)
}

// mustSign produces a compact RS256 JWS over claims. It runs inside the fake
// IdP's handler goroutine, so it panics rather than taking a *testing.T.
func mustSign(key *rsa.PrivateKey, claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": testKID})
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	input := b64(header) + "." + b64(payload)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return input + "." + b64(sig)
}

func writeTokenResponse(w http.ResponseWriter, idToken string) {
	body := map[string]any{
		"access_token": "at-123",
		"token_type":   "Bearer",
		"expires_in":   3600,
	}
	if idToken != "" {
		body["id_token"] = idToken
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeIdP) lastTokenRequest(t *testing.T) url.Values {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tokenRequests) == 0 {
		t.Fatal("token endpoint was never called")
	}
	return f.tokenRequests[len(f.tokenRequests)-1]
}

func newAuthenticator(t *testing.T, f *fakeIdP, allowUnverified bool) Authenticator {
	t.Helper()
	a, err := New(context.Background(), Settings{
		Issuer:               f.issuer(),
		ClientID:             testClientID,
		ClientSecret:         testClientSecret,
		RedirectURL:          testRedirectURL,
		AllowUnverifiedEmail: allowUnverified,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestNew_DiscoversProviderEndpoints(t *testing.T) {
	f := newFakeIdP(t)
	a := newAuthenticator(t, f, false)
	p, ok := a.(*provider)
	if !ok {
		t.Fatalf("New returned %T, want *provider", a)
	}
	if got, want := p.oauth.Endpoint.AuthURL, f.srv.URL+"/authorize"; got != want {
		t.Errorf("AuthURL = %q, want %q", got, want)
	}
	if got, want := p.oauth.Endpoint.TokenURL, f.srv.URL+"/token"; got != want {
		t.Errorf("TokenURL = %q, want %q", got, want)
	}
	if p.oauth.ClientID != testClientID || p.oauth.ClientSecret != testClientSecret || p.oauth.RedirectURL != testRedirectURL {
		t.Errorf("oauth config = %+v", p.oauth)
	}
	if strings.Join(p.oauth.Scopes, " ") != "openid email" {
		t.Errorf("scopes = %v, want [openid email]", p.oauth.Scopes)
	}
	if p.allowUnverifiedEmail {
		t.Error("allowUnverifiedEmail should default to false")
	}
}

func TestNew_DiscoveryFailures(t *testing.T) {
	t.Run("discovery endpoint missing", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(srv.Close)
		_, err := New(context.Background(), Settings{Issuer: srv.URL, ClientID: testClientID})
		if err == nil || !strings.Contains(err.Error(), "oidc discover "+srv.URL) {
			t.Fatalf("err = %v, want wrapped discovery error naming the issuer", err)
		}
	})
	t.Run("issuer mismatch in discovery document", func(t *testing.T) {
		f := newFakeIdP(t)
		f.issuerOverride = "https://evil.example.test"
		_, err := New(context.Background(), Settings{Issuer: f.issuer(), ClientID: testClientID})
		if err == nil || !strings.Contains(err.Error(), "oidc discover") {
			t.Fatalf("err = %v, want discovery error for mismatched issuer", err)
		}
	})
	t.Run("unreachable issuer", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		addr := srv.URL
		srv.Close()
		if _, err := New(context.Background(), Settings{Issuer: addr, ClientID: testClientID}); err == nil {
			t.Fatal("expected an error for an unreachable issuer")
		}
	})
}

func TestAuthCodeURL_BindsStateAndPKCE(t *testing.T) {
	f := newFakeIdP(t)
	a := newAuthenticator(t, f, false)
	verifier := GenerateVerifier()
	raw := a.AuthCodeURL("state-xyz", verifier)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != f.srv.URL+"/authorize" {
		t.Errorf("auth endpoint = %q", got)
	}
	q := u.Query()
	sum := sha256.Sum256([]byte(verifier))
	want := map[string]string{
		"response_type":         "code",
		"client_id":             testClientID,
		"redirect_uri":          testRedirectURL,
		"scope":                 "openid email",
		"state":                 "state-xyz",
		"code_challenge_method": "S256",
		"code_challenge":        b64(sum[:]),
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	// The verifier itself is the secret half of PKCE and must never appear in
	// the front-channel redirect.
	if strings.Contains(raw, verifier) {
		t.Error("AuthCodeURL leaked the PKCE verifier")
	}
	if q.Has("code_verifier") || q.Has("client_secret") {
		t.Errorf("unexpected secret parameters in %q", raw)
	}
}

func TestGenerateVerifier_IsFreshAndRFC7636Shaped(t *testing.T) {
	a, b := GenerateVerifier(), GenerateVerifier()
	if a == b {
		t.Fatal("two verifiers were identical")
	}
	// RFC 7636 §4.1: 43..128 chars of the unreserved set.
	if len(a) < 43 || len(a) > 128 {
		t.Fatalf("verifier length %d outside 43..128", len(a))
	}
	for _, r := range a {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)) {
			t.Fatalf("verifier %q has non-unreserved rune %q", a, r)
		}
	}
}

func TestExchange_Success(t *testing.T) {
	f := newFakeIdP(t)
	a := newAuthenticator(t, f, false)
	verifier := GenerateVerifier()

	claims, err := a.Exchange(context.Background(), "auth-code-1", verifier)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if claims.Email != "alice@acme.test" || claims.Subject != "user-123" {
		t.Fatalf("claims = %+v", claims)
	}
	form := f.lastTokenRequest(t)
	if got := form.Get("grant_type"); got != "authorization_code" {
		t.Errorf("grant_type = %q", got)
	}
	if got := form.Get("code"); got != "auth-code-1" {
		t.Errorf("code = %q", got)
	}
	if got := form.Get("code_verifier"); got != verifier {
		t.Errorf("code_verifier = %q, want the verifier passed to Exchange", got)
	}
	if got := form.Get("redirect_uri"); got != testRedirectURL {
		t.Errorf("redirect_uri = %q", got)
	}
}

func TestExchange_StringEmailVerifiedAccepted(t *testing.T) {
	f := newFakeIdP(t)
	f.idClaims = func(f *fakeIdP) map[string]any {
		c := f.validClaims()
		c["email_verified"] = "true"
		return c
	}
	a := newAuthenticator(t, f, false)
	if _, err := a.Exchange(context.Background(), "code", GenerateVerifier()); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
}

func TestExchange_EmailVerificationGate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(map[string]any)
		allow   bool
		wantErr bool
	}{
		{"unverified rejected", func(c map[string]any) { c["email_verified"] = false }, false, true},
		{"claim absent rejected", func(c map[string]any) { delete(c, "email_verified") }, false, true},
		{"unverified allowed when configured", func(c map[string]any) { c["email_verified"] = false }, true, false},
		{"absent allowed when configured", func(c map[string]any) { delete(c, "email_verified") }, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIdP(t)
			f.idClaims = func(f *fakeIdP) map[string]any {
				c := f.validClaims()
				tc.mutate(c)
				return c
			}
			a := newAuthenticator(t, f, tc.allow)
			claims, err := a.Exchange(context.Background(), "code", GenerateVerifier())
			if tc.wantErr {
				if !errors.Is(err, ErrEmailUnverified) {
					t.Fatalf("err = %v, want ErrEmailUnverified", err)
				}
				if claims != nil {
					t.Fatalf("claims = %+v, want nil on error", claims)
				}
				return
			}
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if claims.Email != "alice@acme.test" {
				t.Fatalf("claims = %+v", claims)
			}
		})
	}
}

func TestExchange_MissingEmailClaim(t *testing.T) {
	f := newFakeIdP(t)
	f.idClaims = func(f *fakeIdP) map[string]any {
		c := f.validClaims()
		delete(c, "email")
		delete(c, "email_verified")
		return c
	}
	a := newAuthenticator(t, f, false)
	if _, err := a.Exchange(context.Background(), "code", GenerateVerifier()); !errors.Is(err, ErrEmailUnverified) {
		t.Fatalf("err = %v, want ErrEmailUnverified for a token with no email", err)
	}
}

func TestExchange_MalformedEmailVerifiedClaim(t *testing.T) {
	f := newFakeIdP(t)
	f.idClaims = func(f *fakeIdP) map[string]any {
		c := f.validClaims()
		c["email_verified"] = "yes"
		return c
	}
	a := newAuthenticator(t, f, true)
	_, err := a.Exchange(context.Background(), "code", GenerateVerifier())
	if err == nil || !strings.Contains(err.Error(), "oidc: parse claims") {
		t.Fatalf("err = %v, want a claim parse error", err)
	}
}

// TestExchange_IDTokenVerificationFailures covers the signature, issuer,
// audience and expiry checks: each must surface as an id_token verify error
// and never yield claims.
func TestExchange_IDTokenVerificationFailures(t *testing.T) {
	_, rogueKey := testKeys(t)
	cases := []struct {
		name  string
		setup func(f *fakeIdP)
		check func(t *testing.T, err error)
	}{
		{
			name:  "bad signature",
			setup: func(f *fakeIdP) { f.signWith = rogueKey },
		},
		{
			name: "wrong audience",
			setup: func(f *fakeIdP) {
				f.idClaims = func(f *fakeIdP) map[string]any {
					c := f.validClaims()
					c["aud"] = "some-other-client"
					return c
				}
			},
		},
		{
			name: "wrong issuer",
			setup: func(f *fakeIdP) {
				f.idClaims = func(f *fakeIdP) map[string]any {
					c := f.validClaims()
					c["iss"] = "https://evil.example.test"
					return c
				}
			},
		},
		{
			name: "expired",
			setup: func(f *fakeIdP) {
				f.idClaims = func(f *fakeIdP) map[string]any {
					c := f.validClaims()
					c["iat"] = time.Now().Add(-2 * time.Hour).Unix()
					c["exp"] = time.Now().Add(-time.Hour).Unix()
					return c
				}
			},
			check: func(t *testing.T, err error) {
				var expired *oidc.TokenExpiredError
				if !errors.As(err, &expired) {
					t.Errorf("err = %v, want *oidc.TokenExpiredError in the chain", err)
				}
			},
		},
		{
			name: "not a JWT",
			setup: func(f *fakeIdP) {
				f.tokenHandler = func(w http.ResponseWriter, _ *fakeIdP) { writeTokenResponse(w, "not-a-jwt") }
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIdP(t)
			a := newAuthenticator(t, f, false)
			tc.setup(f)
			claims, err := a.Exchange(context.Background(), "code", GenerateVerifier())
			if err == nil || !strings.Contains(err.Error(), "oidc: id_token verify") {
				t.Fatalf("err = %v, want id_token verify error", err)
			}
			if errors.Is(err, ErrEmailUnverified) {
				t.Fatalf("err = %v, verification failure misreported as unverified email", err)
			}
			if claims != nil {
				t.Fatalf("claims = %+v, want nil", claims)
			}
			if tc.check != nil {
				tc.check(t, err)
			}
		})
	}
}

func TestExchange_TokenEndpointFailures(t *testing.T) {
	cases := []struct {
		name    string
		handler func(w http.ResponseWriter, f *fakeIdP)
		wantSub string
	}{
		{
			name: "oauth error response",
			handler: func(w http.ResponseWriter, _ *fakeIdP) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"code expired"}`))
			},
			wantSub: "oidc code exchange",
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *fakeIdP) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
			wantSub: "oidc code exchange",
		},
		{
			name:    "no id_token",
			handler: func(w http.ResponseWriter, _ *fakeIdP) { writeTokenResponse(w, "") },
			wantSub: "no id_token",
		},
		{
			name: "id_token not a string",
			handler: func(w http.ResponseWriter, _ *fakeIdP) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"at","token_type":"Bearer","id_token":42}`))
			},
			wantSub: "no id_token",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIdP(t)
			a := newAuthenticator(t, f, false)
			f.tokenHandler = tc.handler
			claims, err := a.Exchange(context.Background(), "code", GenerateVerifier())
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantSub)
			}
			if claims != nil {
				t.Fatalf("claims = %+v, want nil", claims)
			}
		})
	}
}

func TestExchange_HonoursContextCancellation(t *testing.T) {
	f := newFakeIdP(t)
	a := newAuthenticator(t, f, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Exchange(ctx, "code", GenerateVerifier()); err == nil {
		t.Fatal("Exchange with a cancelled context should fail")
	}
}

func TestFlexibleBool_RejectsUnexpectedValues(t *testing.T) {
	for _, in := range []string{`1`, `"TRUE"`, `"yes"`, `{}`} {
		var b flexibleBool
		if err := b.UnmarshalJSON([]byte(in)); err == nil {
			t.Errorf("UnmarshalJSON(%s) = nil error, want rejection", in)
		}
	}
}
