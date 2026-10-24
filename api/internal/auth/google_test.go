package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/oidctest"
)

const testRedirect = "http://localhost:3000/api/auth/google/callback"

func newTestGoogle(t *testing.T) (*Google, *oidctest.Provider) {
	t.Helper()
	p := oidctest.New(t)
	g, err := NewGoogle(context.Background(), p.URL(), p.ClientID, p.ClientSecret, testRedirect)
	if err != nil {
		t.Fatalf("NewGoogle: %v", err)
	}
	return g, p
}

// authorize follows AuthURL to the fake provider and returns the code it
// sends back, as a browser would.
func authorize(t *testing.T, g *Google, fs FlowState) string {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(g.AuthURL(fs))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: status %d, location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if loc.Query().Get("state") != fs.State {
		t.Fatalf("state not echoed back")
	}
	return loc.Query().Get("code")
}

func TestExchangeHappyPath(t *testing.T) {
	g, _ := newTestGoogle(t)
	fs, _ := NewFlowState()

	id, err := g.Exchange(context.Background(), authorize(t, g, fs), fs)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if id.Sub != oidctest.DefaultSub || id.Email != oidctest.DefaultEmail {
		t.Fatalf("identity = %+v", id)
	}
}

func TestExchangeRejects(t *testing.T) {
	cases := []struct {
		name    string
		claims  map[string]any
		foreign bool
		want    error
	}{
		{name: "wrong nonce", claims: map[string]any{"nonce": "someone-elses"}, want: ErrInvalidToken},
		{name: "wrong aud", claims: map[string]any{"aud": "another-client"}, want: ErrInvalidToken},
		{name: "wrong iss", claims: map[string]any{"iss": "https://evil.example"}, want: ErrInvalidToken},
		{name: "expired", claims: map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}, want: ErrInvalidToken},
		{name: "missing sub", claims: map[string]any{"sub": nil}, want: ErrInvalidToken},
		{name: "foreign signing key", foreign: true, want: ErrInvalidToken},
		{name: "email not verified", claims: map[string]any{"email_verified": false}, want: ErrEmailUnverified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, p := newTestGoogle(t)
			fs, _ := NewFlowState()
			code := authorize(t, g, fs)
			p.Next(tc.claims)
			if tc.foreign {
				p.SignNextWithForeignKey()
			}

			id, err := g.Exchange(context.Background(), code, fs)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v (identity %+v), want %v", err, id, tc.want)
			}
		})
	}
}

func TestExchangeWrongVerifier(t *testing.T) {
	g, _ := newTestGoogle(t)
	fs, _ := NewFlowState()
	code := authorize(t, g, fs)

	other, _ := NewFlowState()
	fs.Verifier = other.Verifier
	if _, err := g.Exchange(context.Background(), code, fs); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestAuthURLParams(t *testing.T) {
	g, _ := newTestGoogle(t)
	fs, _ := NewFlowState()
	u, err := url.Parse(g.AuthURL(fs))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	want := map[string]string{
		"scope":                 "openid email",
		"code_challenge_method": "S256",
		"state":                 fs.State,
		"nonce":                 fs.Nonce,
		"prompt":                "select_account",
		"redirect_uri":          testRedirect,
		"response_type":         "code",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge") == fs.Verifier {
		t.Errorf("code_challenge = %q: want the S256 hash, not the verifier", q.Get("code_challenge"))
	}
	if q.Has("access_type") {
		t.Errorf("access_type = %q: no refresh token is ever requested", q.Get("access_type"))
	}
}
