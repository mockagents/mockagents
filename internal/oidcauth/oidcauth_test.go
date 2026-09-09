package oidcauth

import (
	"encoding/json"
	"errors"
	"testing"
)

// decodeJSON stands in for idToken.Claims: it unmarshals a fixed payload into
// the caller's struct.
func decodeJSON(payload string) func(any) error {
	return func(v any) error { return json.Unmarshal([]byte(payload), v) }
}

// TestClaimsFrom_RequiresVerifiedEmail is the audit H-05 guard: the callback
// maps the email's domain to a tenant, so an address the issuer has not
// verified must not reach it.
func TestClaimsFrom_RequiresVerifiedEmail(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		allow   bool
		wantErr bool
	}{
		{"verified bool", `{"email":"a@acme.com","email_verified":true}`, false, false},
		{"verified string", `{"email":"a@acme.com","email_verified":"true"}`, false, false},
		{"explicitly unverified", `{"email":"a@acme.com","email_verified":false}`, false, true},
		{"unverified string", `{"email":"a@acme.com","email_verified":"false"}`, false, true},
		{"claim absent", `{"email":"a@acme.com"}`, false, true},
		{"claim null", `{"email":"a@acme.com","email_verified":null}`, false, true},
		{"absent but allowed", `{"email":"a@acme.com"}`, true, false},
		{"false but allowed", `{"email":"a@acme.com","email_verified":false}`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims, err := claimsFrom(decodeJSON(tc.payload), "sub-1", tc.allow)
			if tc.wantErr {
				if !errors.Is(err, ErrEmailUnverified) {
					t.Fatalf("err = %v, want ErrEmailUnverified", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if claims.Email != "a@acme.com" || claims.Subject != "sub-1" {
				t.Fatalf("claims = %+v", claims)
			}
		})
	}
}

// TestClaimsFrom_MalformedVerifiedClaim: a value that is neither a boolean nor
// its string form is a parse error, not silently "verified".
func TestClaimsFrom_MalformedVerifiedClaim(t *testing.T) {
	_, err := claimsFrom(decodeJSON(`{"email":"a@acme.com","email_verified":"yes"}`), "sub", true)
	if err == nil || errors.Is(err, ErrEmailUnverified) {
		t.Fatalf("err = %v, want a parse error", err)
	}
}
