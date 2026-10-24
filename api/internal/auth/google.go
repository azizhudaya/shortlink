package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// GoogleIssuer is the issuer Google's ID tokens carry.
const GoogleIssuer = "https://accounts.google.com"

var (
	// ErrInvalidToken covers every failed exchange or verification. The
	// wrapped cause is for logs only.
	ErrInvalidToken = errors.New("invalid sign-in")

	// ErrEmailUnverified is a genuine Google identity we still refuse (D10).
	ErrEmailUnverified = errors.New("google email not verified")
)

// Identity is all the service keeps from Google: the stable subject and a
// display copy of the email (D3, D10).
type Identity struct {
	Sub, Email string
}

// Google runs the OIDC authorization-code flow with PKCE and a nonce.
type Google struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// Google's published endpoints, as listed by
// https://accounts.google.com/.well-known/openid-configuration.
const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
	googleJWKSURL  = "https://www.googleapis.com/oauth2/v3/certs"
)

// NewGoogleStatic is what main uses: Google's endpoints are fixed, so nothing
// is fetched at startup and the API (which also serves redirects) boots even
// when Google is unreachable. Signing keys are fetched on first verification.
// The context governs those later key fetches, so main passes a long-lived one.
func NewGoogleStatic(ctx context.Context, clientID, clientSecret, redirectURL string) *Google {
	provider := (&oidc.ProviderConfig{
		IssuerURL:  GoogleIssuer,
		AuthURL:    googleAuthURL,
		TokenURL:   googleTokenURL,
		JWKSURL:    googleJWKSURL,
		Algorithms: []string{oidc.RS256},
	}).NewProvider(ctx)
	return newGoogle(provider, clientID, clientSecret, redirectURL)
}

// NewGoogle fetches the issuer's discovery document first. Tests use it to
// point the client at a fake provider (D18).
func NewGoogle(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL string) (*Google, error) {
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return newGoogle(provider, clientID, clientSecret, redirectURL), nil
}

func newGoogle(provider *oidc.Provider, clientID, clientSecret, redirectURL string) *Google {
	return &Google{
		oauth: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     provider.Endpoint(),
			// No profile scope and no offline access: nothing beyond sub and
			// email is requested, and no Google token outlives the callback.
			Scopes: []string{oidc.ScopeOpenID, "email"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}
}

func (g *Google) AuthURL(fs FlowState) string {
	return g.oauth.AuthCodeURL(fs.State,
		oidc.Nonce(fs.Nonce),
		oauth2.S256ChallengeOption(fs.Verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
}

// Exchange redeems the code and verifies the ID token: signature, issuer,
// audience and expiry (go-oidc), then nonce, subject and email_verified.
func (g *Google) Exchange(ctx context.Context, code string, fs FlowState) (Identity, error) {
	tok, err := g.oauth.Exchange(ctx, code, oauth2.VerifierOption(fs.Verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: code exchange: %v", ErrInvalidToken, err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return Identity{}, fmt.Errorf("%w: no id_token in response", ErrInvalidToken)
	}
	idt, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(fs.Nonce)) != 1 {
		return Identity{}, fmt.Errorf("%w: nonce mismatch", ErrInvalidToken)
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idt.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("%w: claims: %v", ErrInvalidToken, err)
	}
	if idt.Subject == "" || claims.Email == "" {
		return Identity{}, fmt.Errorf("%w: missing sub or email", ErrInvalidToken)
	}
	if !claims.EmailVerified {
		return Identity{}, ErrEmailUnverified
	}
	return Identity{Sub: idt.Subject, Email: claims.Email}, nil
}
