package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/azizhudaya/shortlink/api/internal/auth"
	"github.com/azizhudaya/shortlink/api/internal/config"
	"github.com/azizhudaya/shortlink/api/internal/oidctest"
	"github.com/azizhudaya/shortlink/api/internal/store"
	"github.com/azizhudaya/shortlink/api/internal/testdb"
)

// testEnv is the API served over HTTP against a real test database and a
// fake Google, as the integration tests for sign-in and links see it.
type testEnv struct {
	srv      *httptest.Server
	server   *Server
	provider *oidctest.Provider
	pool     *pgxpool.Pool
	store    *store.Postgres
	checker  *fakeChecker
	logs     *syncBuffer
}

type fakeChecker struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (f *fakeChecker) Check(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newEnv(t *testing.T, mutate ...func(*config.Config)) *testEnv {
	t.Helper()
	pool := testdb.New(t)
	st := store.NewPostgres(pool)
	p := oidctest.New(t)

	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		PublicHostnames:   []string{"afh.my.id", "app.afh.my.id"},
		SignupsOpen:       true,
		GoogleRedirectURL: srv.URL + "/api/auth/google/callback",
	}
	for _, m := range mutate {
		m(&cfg)
	}
	g, err := auth.NewGoogle(context.Background(), p.URL(), p.ClientID, p.ClientSecret, cfg.GoogleRedirectURL)
	if err != nil {
		t.Fatal(err)
	}

	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	prev := slog.Default()
	slog.SetDefault(logger) // AccessLog and Recover log through the default
	t.Cleanup(func() { slog.SetDefault(prev) })

	checker := &fakeChecker{}
	s := NewServer(cfg, st, st, g, checker, logger)
	handler = s.Routes()
	return &testEnv{srv: srv, server: s, provider: p, pool: pool, store: st, checker: checker, logs: logs}
}

var noFollow = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// request sends one request to the API without following redirects.
func (e *testEnv) request(t *testing.T, method, path, body string, header map[string]string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	target := path
	if strings.HasPrefix(path, "/") {
		target = e.srv.URL + path
	}
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, target, r)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// flow is a sign-in stopped just before the callback: the browser holds the
// oauth cookie and Google has sent it back to callbackURL.
type flow struct {
	oauth       *http.Cookie
	callbackURL string
}

func (e *testEnv) begin(t *testing.T) flow {
	t.Helper()
	start := e.request(t, "GET", "/api/auth/google/start", "", nil)
	if start.StatusCode != http.StatusFound {
		t.Fatalf("start: status %d", start.StatusCode)
	}
	oauth := cookieNamed(start, "__Host-sl_oauth")
	if oauth == nil {
		t.Fatal("start set no oauth cookie")
	}
	authz := e.request(t, "GET", start.Header.Get("Location"), "", nil)
	if authz.StatusCode != http.StatusFound {
		t.Fatalf("authorize: status %d", authz.StatusCode)
	}
	return flow{oauth: oauth, callbackURL: authz.Header.Get("Location")}
}

func (e *testEnv) finish(t *testing.T, f flow) *http.Response {
	t.Helper()
	return e.request(t, "GET", f.callbackURL, "", nil, f.oauth)
}

// signIn runs the whole flow for one Google identity.
func (e *testEnv) signIn(t *testing.T, sub, email string) *http.Response {
	t.Helper()
	e.provider.Next(map[string]any{"sub": sub, "email": email})
	return e.finish(t, e.begin(t))
}

// session signs in and returns the session cookie and its CSRF token.
func (e *testEnv) session(t *testing.T, sub, email string) (*http.Cookie, string) {
	t.Helper()
	resp := e.signIn(t, sub, email)
	c := cookieNamed(resp, "__Host-sl_session")
	if c == nil {
		t.Fatalf("sign-in as %s failed: %s", sub, resp.Header.Get("Location"))
	}
	me := e.request(t, "GET", "/api/auth/me", "", nil, c)
	var body struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.NewDecoder(me.Body).Decode(&body)
	return c, body.CSRF
}

func (e *testEnv) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func wantLocation(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
		t.Fatalf("got %d → %q, want 302 → %q", resp.StatusCode, resp.Header.Get("Location"), want)
	}
}

func TestFirstSignInCreatesAccount(t *testing.T) {
	e := newEnv(t)
	resp := e.signIn(t, "sub-1", "one@example.com")
	wantLocation(t, resp, "/links")

	c := cookieNamed(resp, "__Host-sl_session")
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" ||
		c.MaxAge != 2592000 || c.Domain != "" {
		t.Fatalf("session cookie = %+v", c)
	}
	me := e.request(t, "GET", "/api/auth/me", "", nil, c)
	var body struct {
		Email string `json:"email"`
		CSRF  string `json:"csrf_token"`
	}
	if err := json.NewDecoder(me.Body).Decode(&body); err != nil || me.StatusCode != 200 {
		t.Fatalf("/me: %d, %v", me.StatusCode, err)
	}
	if body.Email != "one@example.com" || body.CSRF != auth.CSRFToken(c.Value) {
		t.Fatalf("/me = %+v", body)
	}
	if n := e.count(t, `SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("users = %d", n)
	}
}

func TestSecondSignInSameAccount(t *testing.T) {
	e := newEnv(t)
	wantLocation(t, e.signIn(t, "sub-1", "one@example.com"), "/links")
	wantLocation(t, e.signIn(t, "sub-1", "one@example.com"), "/links")
	if n := e.count(t, `SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM sessions`); n != 2 {
		t.Fatalf("sessions = %d, want 2", n)
	}
}

func TestSignupsClosed(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.SignupsOpen = false })
	if _, _, err := e.store.UpsertGoogleUser(context.Background(), "existing", "old@example.com", true); err != nil {
		t.Fatal(err)
	}

	wantLocation(t, e.signIn(t, "newcomer", "new@example.com"), "/signin?error=signups_closed")
	if n := e.count(t, `SELECT count(*) FROM users WHERE google_sub = 'newcomer'`); n != 0 {
		t.Fatal("closed sign-ups created a user")
	}
	wantLocation(t, e.signIn(t, "existing", "old@example.com"), "/links")
}

func TestUnverifiedEmail(t *testing.T) {
	e := newEnv(t)
	f := e.begin(t)
	e.provider.Next(map[string]any{"email_verified": false})
	wantLocation(t, e.finish(t, f), "/signin?error=email_unverified")
	if n := e.count(t, `SELECT count(*) FROM users`); n != 0 {
		t.Fatalf("users = %d", n)
	}
}

func TestDisabledAccount(t *testing.T) {
	e := newEnv(t)
	wantLocation(t, e.signIn(t, "sub-1", "one@example.com"), "/links")
	if _, _, _, err := e.store.BanUser(context.Background(), "one@example.com", "aziz", "test"); err != nil {
		t.Fatal(err)
	}

	resp := e.signIn(t, "sub-1", "one@example.com")
	wantLocation(t, resp, "/signin?error=account_disabled")
	if cookieNamed(resp, "__Host-sl_session") != nil || e.count(t, `SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("disabled account got a session")
	}
}

func TestCallbackForgery(t *testing.T) {
	cases := map[string]func(e *testEnv, f *flow){
		"no oauth cookie": func(_ *testEnv, f *flow) { f.oauth = nil },
		"altered state": func(_ *testEnv, f *flow) {
			u, _ := url.Parse(f.callbackURL)
			q := u.Query()
			q.Set("state", "attacker-state")
			u.RawQuery = q.Encode()
			f.callbackURL = u.String()
		},
		"different nonce": func(e *testEnv, _ *flow) { e.provider.Next(map[string]any{"nonce": "attacker-nonce"}) },
		"different aud":   func(e *testEnv, _ *flow) { e.provider.Next(map[string]any{"aud": "another-client"}) },
		"garbled oauth cookie": func(_ *testEnv, f *flow) {
			f.oauth = &http.Cookie{Name: f.oauth.Name, Value: "not-a-flow-state"}
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			f := e.begin(t)
			tamper(e, &f)
			resp := e.finish(t, f)
			wantLocation(t, resp, "/signin?error=failed")
			if e.count(t, `SELECT count(*) FROM sessions`) != 0 || e.count(t, `SELECT count(*) FROM users`) != 0 {
				t.Fatal("forged callback created a session or user")
			}
		})
	}
}

func TestCallbackClearsOAuthCookie(t *testing.T) {
	e := newEnv(t)
	ok := e.signIn(t, "sub-1", "one@example.com")
	f := e.begin(t)
	f.callbackURL = strings.Replace(f.callbackURL, "state=", "state=x", 1)
	failed := e.finish(t, f)

	for name, resp := range map[string]*http.Response{"success": ok, "failure": failed} {
		c := cookieNamed(resp, "__Host-sl_oauth")
		if c == nil || c.MaxAge >= 0 {
			t.Errorf("%s: oauth cookie not cleared: %+v", name, c)
		}
	}
}

func TestUserCancelled(t *testing.T) {
	e := newEnv(t)
	f := e.begin(t)
	u, _ := url.Parse(f.callbackURL)
	u.RawQuery = url.Values{"error": {"access_denied"}, "state": {u.Query().Get("state")}}.Encode()
	f.callbackURL = u.String()
	wantLocation(t, e.finish(t, f), "/signin")
}

func TestMeRequiresSession(t *testing.T) {
	e := newEnv(t)
	for _, c := range []*http.Cookie{nil, {Name: "__Host-sl_session", Value: "made-up"}} {
		if resp := e.request(t, "GET", "/api/auth/me", "", nil, c); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("cookie %v: status %d, want 401", c, resp.StatusCode)
		}
	}
}

func TestLogoutRequiresCSRF(t *testing.T) {
	e := newEnv(t)
	c, csrf := e.session(t, "sub-1", "one@example.com")

	if resp := e.request(t, "POST", "/api/auth/logout", "", nil, c); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("without CSRF: %d, want 403", resp.StatusCode)
	}
	resp := e.request(t, "POST", "/api/auth/logout", "", map[string]string{"X-CSRF-Token": csrf}, c)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("with CSRF: %d, want 204", resp.StatusCode)
	}
	if cleared := cookieNamed(resp, "__Host-sl_session"); cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("session cookie not cleared: %+v", cleared)
	}
	if resp := e.request(t, "GET", "/api/auth/me", "", nil, c); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old cookie after logout: %d, want 401", resp.StatusCode)
	}
	if n := e.count(t, `SELECT count(*) FROM sessions`); n != 0 {
		t.Fatalf("sessions = %d after logout", n)
	}
}

func TestSecretsNeverLogged(t *testing.T) {
	e := newEnv(t)
	f := e.begin(t)
	cb, _ := url.Parse(f.callbackURL)
	fs, err := auth.DecodeFlowState(f.oauth.Value)
	if err != nil {
		t.Fatal(err)
	}
	resp := e.finish(t, f)
	c := cookieNamed(resp, "__Host-sl_session")
	csrf := auth.CSRFToken(c.Value)
	e.request(t, "GET", "/api/auth/me", "", nil, c)
	e.request(t, "POST", "/api/auth/logout", "", map[string]string{"X-CSRF-Token": csrf}, c)
	// A failed callback logs its cause; the cause must not carry secrets either.
	f2 := e.begin(t)
	e.provider.Next(map[string]any{"nonce": "attacker-nonce"})
	e.finish(t, f2)

	logs := e.logs.String()
	if !strings.Contains(logs, `"user_id"`) {
		t.Fatalf("expected sign-in to be logged; logs:\n%s", logs)
	}
	secrets := map[string]string{
		"session token": c.Value, "csrf token": csrf, "code": cb.Query().Get("code"),
		"state": fs.State, "nonce": fs.Nonce, "verifier": fs.Verifier, "client secret": e.provider.ClientSecret,
	}
	for name, v := range secrets {
		if v == "" || strings.Contains(logs, v) {
			t.Errorf("%s (%q) appears in logs or is empty", name, v)
		}
	}
}
