// Package oidctest is a fake OpenID Connect provider for tests: discovery,
// JWKS, an /authorize that approves instantly, and a /token that enforces
// PKCE. It signs RS256 ID tokens with a throwaway per-test key, so sign-in
// can be tested offline and deterministically (D18).
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

const keyID = "test-key"

type Provider struct {
	ClientID     string
	ClientSecret string

	srv     *httptest.Server
	key     *rsa.PrivateKey
	foreign *rsa.PrivateKey

	mu          sync.Mutex
	grants      map[string]grant // code → what /authorize saw
	next        map[string]any
	nextForeign bool
}

type grant struct {
	nonce, challenge string
}

// DefaultSub and DefaultEmail are the identity in every ID token unless Next
// overrides them.
const (
	DefaultSub   = "google-sub-1"
	DefaultEmail = "user@example.com"
)

func New(t *testing.T) *Provider {
	t.Helper()
	p := &Provider{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		key:          newKey(t),
		foreign:      newKey(t),
		grants:       map[string]grant{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

// URL is the issuer.
func (p *Provider) URL() string { return p.srv.URL }

// Next merges claims over the defaults for the next ID token only. A nil
// value removes the claim.
func (p *Provider) Next(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next = claims
}

// SignNextWithForeignKey makes the next ID token carry the right key ID but a
// signature from a key the JWKS does not publish.
func (p *Provider) SignNextWithForeignKey() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextForeign = true
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                p.URL(),
		"authorization_endpoint":                p.URL() + "/authorize",
		"token_endpoint":                        p.URL() + "/token",
		"jwks_uri":                              p.URL() + "/jwks",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &p.key.PublicKey, KeyID: keyID, Algorithm: "RS256", Use: "sig",
	}}})
}

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || q.Get("client_id") != p.ClientID || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "bad authorize request", http.StatusBadRequest)
		return
	}
	code := randomString()
	p.mu.Lock()
	p.grants[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
	p.mu.Unlock()

	back := redirect.Query()
	back.Set("code", code)
	back.Set("state", q.Get("state"))
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != p.ClientID || secret != p.ClientSecret {
		tokenError(w, "invalid_client")
		return
	}

	p.mu.Lock()
	g, found := p.grants[r.PostForm.Get("code")]
	delete(p.grants, r.PostForm.Get("code")) // codes are single-use
	claims, foreign := p.next, p.nextForeign
	p.next, p.nextForeign = nil, false
	p.mu.Unlock()

	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		tokenError(w, "invalid_grant")
		return
	}

	now := time.Now()
	all := map[string]any{
		"iss":            p.URL(),
		"aud":            p.ClientID,
		"sub":            DefaultSub,
		"email":          DefaultEmail,
		"email_verified": true,
		"nonce":          g.nonce,
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	}
	for k, v := range claims {
		if v == nil {
			delete(all, k)
		} else {
			all[k] = v
		}
	}

	key := p.key
	if foreign {
		key = p.foreign
	}
	writeJSON(w, map[string]any{
		"access_token": "fake-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     sign(key, all),
	})
}

func sign(key *rsa.PrivateKey, claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.RS256,
		Key:       jose.JSONWebKey{Key: key, KeyID: keyID},
	}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		panic(err)
	}
	payload, _ := json.Marshal(claims)
	jws, err := signer.Sign(payload)
	if err != nil {
		panic(err)
	}
	out, err := jws.CompactSerialize()
	if err != nil {
		panic(err)
	}
	return out
}

func tokenError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func randomString() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
