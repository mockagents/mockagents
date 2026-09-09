// Package oidcauth is the OpenID Connect relying-party seam for SSO login
// (REF-08 slice D). It wraps coreos/go-oidc + x/oauth2 behind a small
// Authenticator interface so the login/callback handlers can be unit-tested
// with a fake provider — no live IdP needed.
package oidcauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Claims are the verified identity fields extracted from an OIDC ID token.
type Claims struct {
	Email   string
	Subject string
}

// GenerateVerifier returns a fresh PKCE code verifier. Exposed so callers don't
// need to import x/oauth2 directly — the verifier is opaque to them and round-
// trips through AuthCodeURL (login) and Exchange (callback).
func GenerateVerifier() string { return oauth2.GenerateVerifier() }

// Authenticator runs the OIDC authorization-code flow with PKCE. The handlers
// depend only on this interface; the real impl talks to a provider, a fake one
// drives tests.
type Authenticator interface {
	// AuthCodeURL builds the provider redirect URL for a login, binding the
	// given state and PKCE verifier (the impl derives the S256 challenge).
	AuthCodeURL(state, verifier string) string
	// Exchange swaps an authorization code (with the PKCE verifier) for a
	// validated ID token and returns its claims.
	Exchange(ctx context.Context, code, verifier string) (*Claims, error)
}

// Settings configures the real provider-backed Authenticator.
type Settings struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// AllowUnverifiedEmail accepts an ID token whose email_verified claim is
	// absent or false. Off by default: the callback maps the email's DOMAIN
	// to a tenant, so an unverified address is a tenant-membership claim the
	// issuer has not vouched for — on an IdP with self-service or social
	// sign-up, anyone can pick "attacker@acme.com" (audit H-05). Turn this on
	// only for an issuer that never emits the claim and whose directory is
	// the sole source of accounts (some enterprise Entra ID tenants).
	AllowUnverifiedEmail bool
}

// provider is the production Authenticator backed by a discovered OIDC provider.
type provider struct {
	oauth                oauth2.Config
	verifier             *oidc.IDTokenVerifier
	allowUnverifiedEmail bool
}

// New discovers the OIDC provider at Settings.Issuer and returns an
// Authenticator. It requires network access to the issuer's discovery
// document, so it is constructed once at startup.
func New(ctx context.Context, s Settings) (Authenticator, error) {
	p, err := oidc.NewProvider(ctx, s.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discover %s: %w", s.Issuer, err)
	}
	return &provider{
		oauth: oauth2.Config{
			ClientID:     s.ClientID,
			ClientSecret: s.ClientSecret,
			RedirectURL:  s.RedirectURL,
			Endpoint:     p.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email"},
		},
		verifier:             p.Verifier(&oidc.Config{ClientID: s.ClientID}),
		allowUnverifiedEmail: s.AllowUnverifiedEmail,
	}, nil
}

func (p *provider) AuthCodeURL(state, verifier string) string {
	return p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

func (p *provider) Exchange(ctx context.Context, code, verifier string) (*Claims, error) {
	tok, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("oidc code exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, fmt.Errorf("oidc: token response had no id_token")
	}
	idToken, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("oidc: id_token verify: %w", err)
	}
	return claimsFrom(idToken.Claims, idToken.Subject, p.allowUnverifiedEmail)
}

// ErrEmailUnverified is returned when the ID token does not carry
// `email_verified: true` and the Authenticator was not configured to accept
// that. The signature, issuer and audience checks say nothing about whether
// the issuer actually confirmed the address.
var ErrEmailUnverified = errors.New("oidc: email is not verified by the issuer")

// flexibleBool decodes the email_verified claim, which the spec defines as a
// JSON boolean but some providers emit as the string "true"/"false".
type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true", `"true"`:
		*b = true
	case "false", `"false"`, "null":
		*b = false
	default:
		return fmt.Errorf("email_verified: unexpected value %s", data)
	}
	return nil
}

// claimsFrom extracts the identity claims through decode (idToken.Claims in
// production) and enforces the email_verified gate. Split out so the gate is
// testable without a live provider.
func claimsFrom(decode func(any) error, subject string, allowUnverified bool) (*Claims, error) {
	var c struct {
		Email         string        `json:"email"`
		EmailVerified *flexibleBool `json:"email_verified"`
	}
	if err := decode(&c); err != nil {
		return nil, fmt.Errorf("oidc: parse claims: %w", err)
	}
	verified := c.EmailVerified != nil && bool(*c.EmailVerified)
	if !verified && !allowUnverified {
		return nil, fmt.Errorf("%w (email %q; set AllowUnverifiedEmail only for an issuer that never emits the claim)", ErrEmailUnverified, c.Email)
	}
	return &Claims{Email: c.Email, Subject: subject}, nil
}
