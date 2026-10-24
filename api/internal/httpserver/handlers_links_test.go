package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/ratelimit"
	"github.com/azizhudaya/shortlink/api/internal/safety"
)

// account is a signed-in user as the UI holds it.
type account struct {
	cookie *http.Cookie
	csrf   string
}

func (e *testEnv) account(t *testing.T, sub string) account {
	t.Helper()
	c, csrf := e.session(t, sub, sub+"@example.com")
	return account{cookie: c, csrf: csrf}
}

// relax lifts the burst limits and quota for tests about something else.
func (e *testEnv) relax() {
	e.server.createPerUser = ratelimit.New(time.Millisecond, 10_000, time.Hour, 100)
	e.server.createPerIP = ratelimit.New(time.Millisecond, 10_000, time.Hour, 100)
	e.server.newAccountCap = 10_000
}

func (e *testEnv) send(t *testing.T, a account, method, path, body string) *http.Response {
	t.Helper()
	h := map[string]string{"X-CSRF-Token": a.csrf}
	if body != "" {
		h["Content-Type"] = "application/json"
	}
	return e.request(t, method, path, body, h, a.cookie)
}

func (e *testEnv) create(t *testing.T, a account, body string) *http.Response {
	t.Helper()
	return e.send(t, a, "POST", "/api/links", body)
}

func (e *testEnv) mustCreate(t *testing.T, a account, body string) createResponse {
	t.Helper()
	resp := e.create(t, a, body)
	var out createResponse
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create %s: status %d, error %s", body, resp.StatusCode, errorCode(resp))
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func errorCode(resp *http.Response) string {
	var b errorBody
	_ = json.NewDecoder(resp.Body).Decode(&b)
	return b.Error.Code
}

func wantError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	if got := errorCode(resp); resp.StatusCode != status || got != code {
		t.Fatalf("got %d %s, want %d %s", resp.StatusCode, got, status, code)
	}
}

func (e *testEnv) redirectStatus(t *testing.T, code string) int {
	t.Helper()
	return e.request(t, "GET", "/"+code, "", nil).StatusCode
}

type listBody struct {
	Links []struct {
		Code     string `json:"code"`
		ShortURL string `json:"short_url"`
		LongURL  string `json:"long_url"`
		IsCustom bool   `json:"is_custom"`
		Status   string `json:"status"`
	} `json:"links"`
	NextCursor *string `json:"next_cursor"`
}

func (e *testEnv) list(t *testing.T, a account, query string) listBody {
	t.Helper()
	resp := e.send(t, a, "GET", "/api/links"+query, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list%s: status %d", query, resp.StatusCode)
	}
	var b listBody
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

const exampleURL = `{"url":"https://example.com/page"}`

func TestCreateRequiresSession(t *testing.T) {
	e := newEnv(t)
	resp := e.request(t, "POST", "/api/links", exampleURL, map[string]string{"Content-Type": "application/json"})
	wantError(t, resp, http.StatusUnauthorized, CodeUnauthorized)
}

func TestCreateRequiresCSRF(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	resp := e.request(t, "POST", "/api/links", exampleURL, map[string]string{"Content-Type": "application/json"}, a.cookie)
	wantError(t, resp, http.StatusForbidden, CodeCSRFInvalid)
}

func TestCreateStampsOwnerViaHTTP(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	link := e.mustCreate(t, a, `{"url":"https://example.com/x","custom_alias":"mine"}`)
	if link.Code != "mine" || link.ShortURL != "https://afh.my.id/mine" {
		t.Fatalf("created %+v", link)
	}
	if n := e.count(t, `SELECT count(*) FROM links l JOIN users u ON u.id = l.user_id WHERE l.short_code = 'mine' AND u.google_sub = 'a'`); n != 1 {
		t.Fatal("link not owned by its creator")
	}
	if e.checker.calls != 1 {
		t.Fatalf("safety checks = %d, want 1", e.checker.calls)
	}
}

func TestCreateUnsafeURL(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	e.checker.err = safety.ErrUnsafe
	wantError(t, e.create(t, a, exampleURL), http.StatusBadRequest, CodeUnsafeURL)
	if n := e.count(t, `SELECT count(*) FROM links`); n != 0 {
		t.Fatal("unsafe link stored")
	}
}

func TestCreateSafetyUnavailable(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	existing := e.mustCreate(t, a, exampleURL)

	e.checker.err = fmt.Errorf("%w: timeout", safety.ErrUnavailable)
	wantError(t, e.create(t, a, `{"url":"https://example.com/other"}`), http.StatusServiceUnavailable, CodeSafetyUnavailable)
	if n := e.count(t, `SELECT count(*) FROM links`); n != 1 {
		t.Fatal("link stored without a safety check")
	}
	if got := e.redirectStatus(t, existing.Code); got != http.StatusFound {
		t.Fatalf("redirect during outage: %d, want 302", got)
	}
}

func TestSafetyNotCalledWhenInvalid(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	wantError(t, e.create(t, a, `{"url":"javascript:alert(1)"}`), http.StatusBadRequest, CodeInvalidURL)
	wantError(t, e.create(t, a, `{"url":"https://example.com","custom_alias":"api"}`), http.StatusBadRequest, CodeInvalidAlias)
	if e.checker.calls != 0 {
		t.Fatalf("safety checks = %d, want 0", e.checker.calls)
	}
}

func TestNewAccountQuota(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	var codes []string
	for i := 0; i < 10; i++ {
		codes = append(codes, e.mustCreate(t, a, exampleURL).Code)
	}
	for _, c := range codes {
		if resp := e.send(t, a, "DELETE", "/api/links/"+c, ""); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete: %d", resp.StatusCode)
		}
	}

	resp := e.create(t, a, exampleURL)
	retry, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
	wantError(t, resp, http.StatusTooManyRequests, CodeQuotaExceeded)
	if retry <= 0 || retry > 24*3600 {
		t.Fatalf("Retry-After = %q", resp.Header.Get("Retry-After"))
	}
}

func TestWhitelistedNoQuota(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	e.server.newAccountCap = 3
	if _, err := e.store.SetWhitelisted(t.Context(), "a@example.com", true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		e.mustCreate(t, a, exampleURL)
	}

	e.checker.err = safety.ErrUnsafe
	wantError(t, e.create(t, a, exampleURL), http.StatusBadRequest, CodeUnsafeURL)

	e.server.createPerUser = ratelimit.New(time.Hour, 1, time.Hour, 100)
	e.checker.err = nil
	e.mustCreate(t, a, exampleURL)
	wantError(t, e.create(t, a, exampleURL), http.StatusTooManyRequests, CodeRateLimited)
}

func TestMatureAccountQuota(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	e.server.newAccountCap = 2
	e.server.quotaCap = 12
	if _, err := e.pool.Exec(t.Context(), `UPDATE users SET created_at = now() - interval '8 days'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		e.mustCreate(t, a, exampleURL)
	}
	wantError(t, e.create(t, a, exampleURL), http.StatusTooManyRequests, CodeQuotaExceeded)
}

func TestCreateRateLimitedPerUser(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	b := e.account(t, "b")
	e.server.createPerUser = ratelimit.New(time.Hour, 2, time.Hour, 100)
	e.mustCreate(t, a, exampleURL)
	e.mustCreate(t, a, exampleURL)

	resp := e.create(t, a, exampleURL)
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
	wantError(t, resp, http.StatusTooManyRequests, CodeRateLimited)
	e.mustCreate(t, b, exampleURL) // another user is not affected
}

func TestListOnlyOwn(t *testing.T) {
	e := newEnv(t)
	a, b := e.account(t, "a"), e.account(t, "b")
	e.mustCreate(t, a, `{"url":"https://example.com/a","custom_alias":"alpha"}`)
	e.mustCreate(t, b, `{"url":"https://example.com/b","custom_alias":"beta"}`)

	got := e.list(t, a, "")
	if len(got.Links) != 1 || got.Links[0].Code != "alpha" || got.NextCursor != nil {
		t.Fatalf("list = %+v", got)
	}
	l := got.Links[0]
	if l.ShortURL != "https://afh.my.id/alpha" || l.LongURL != "https://example.com/a" || !l.IsCustom || l.Status != "active" {
		t.Fatalf("item = %+v", l)
	}
	if resp := e.request(t, "GET", "/api/links", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous list: %d", resp.StatusCode)
	}
}

func TestListPagination(t *testing.T) {
	e := newEnv(t)
	e.relax()
	a := e.account(t, "a")
	for i := 0; i < 51; i++ {
		e.mustCreate(t, a, exampleURL)
	}

	first := e.list(t, a, "")
	if len(first.Links) != 50 || first.NextCursor == nil {
		t.Fatalf("page 1: %d links, cursor %v", len(first.Links), first.NextCursor)
	}
	second := e.list(t, a, "?cursor="+*first.NextCursor)
	if len(second.Links) != 1 || second.NextCursor != nil {
		t.Fatalf("page 2: %d links, cursor %v", len(second.Links), second.NextCursor)
	}
	for _, l := range first.Links {
		if l.Code == second.Links[0].Code {
			t.Fatal("link repeated across pages")
		}
	}
}

func TestListBadLimitAndCursor(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	for _, q := range []string{"?limit=0", "?limit=101", "?limit=x", "?cursor=***"} {
		wantError(t, e.send(t, a, "GET", "/api/links"+q, ""), http.StatusBadRequest, CodeBadRequest)
	}
	if got := e.list(t, a, "?limit=100"); got.Links != nil && len(got.Links) != 0 {
		t.Fatalf("limit=100: %+v", got)
	}
}

func TestCrossAccountPatchAndDelete404(t *testing.T) {
	e := newEnv(t)
	a, b := e.account(t, "a"), e.account(t, "b")
	e.mustCreate(t, a, `{"url":"https://example.com","custom_alias":"mine"}`)

	wantError(t, e.send(t, b, "PATCH", "/api/links/mine", `{"disabled":true}`), http.StatusNotFound, CodeNotFound)
	wantError(t, e.send(t, b, "DELETE", "/api/links/mine", ""), http.StatusNotFound, CodeNotFound)
	if got := e.redirectStatus(t, "mine"); got != http.StatusFound {
		t.Fatalf("A's link after B's attempts: %d, want 302", got)
	}
}

func TestDisableThenRedirect410ThenEnable302(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	e.mustCreate(t, a, `{"url":"https://example.com","custom_alias":"mine"}`)

	if resp := e.send(t, a, "PATCH", "/api/links/mine", `{"disabled":true}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("disable: %d", resp.StatusCode)
	}
	if got := e.redirectStatus(t, "mine"); got != http.StatusGone {
		t.Fatalf("disabled redirect: %d, want 410", got)
	}
	if e.list(t, a, "").Links[0].Status != "disabled" {
		t.Fatal("status not disabled")
	}
	// A disabled alias stays reserved.
	wantError(t, e.create(t, a, `{"url":"https://example.com","custom_alias":"mine"}`), http.StatusConflict, CodeAliasTaken)

	if resp := e.send(t, a, "PATCH", "/api/links/mine", `{"disabled":false}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("enable: %d", resp.StatusCode)
	}
	if got := e.redirectStatus(t, "mine"); got != http.StatusFound {
		t.Fatalf("enabled redirect: %d, want 302", got)
	}
	wantError(t, e.send(t, a, "PATCH", "/api/links/mine", `{}`), http.StatusBadRequest, CodeBadRequest)
	wantError(t, e.request(t, "PATCH", "/api/links/mine", `{"disabled":true}`, map[string]string{"X-CSRF-Token": a.csrf}, a.cookie),
		http.StatusUnsupportedMediaType, CodeBadRequest)
}

func TestDeleteThenRedirect404AndAliasReusable(t *testing.T) {
	e := newEnv(t)
	a, b := e.account(t, "a"), e.account(t, "b")
	e.mustCreate(t, a, `{"url":"https://example.com","custom_alias":"mine"}`)

	if resp := e.send(t, a, "DELETE", "/api/links/mine", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if got := e.redirectStatus(t, "mine"); got != http.StatusNotFound {
		t.Fatalf("deleted redirect: %d, want 404", got)
	}
	e.mustCreate(t, b, `{"url":"https://example.com/b","custom_alias":"mine"}`)
}

func TestTakenDownLinkOwnerCannotEnableOrDelete(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	e.mustCreate(t, a, `{"url":"https://example.com","custom_alias":"mine"}`)
	if err := e.store.Takedown(t.Context(), "mine", "aziz", "phishing"); err != nil {
		t.Fatal(err)
	}

	wantError(t, e.send(t, a, "PATCH", "/api/links/mine", `{"disabled":false}`), http.StatusConflict, CodeLinkTakenDown)
	wantError(t, e.send(t, a, "DELETE", "/api/links/mine", ""), http.StatusConflict, CodeLinkTakenDown)
	if e.list(t, a, "").Links[0].Status != "taken_down" {
		t.Fatal("status not taken_down")
	}
	if got := e.redirectStatus(t, "mine"); got != http.StatusGone {
		t.Fatalf("taken-down redirect: %d, want 410", got)
	}
}

func TestRedirectSetsNoCookie(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	e.mustCreate(t, a, `{"url":"https://example.com","custom_alias":"mine"}`)
	resp := e.request(t, "GET", "/mine", "", nil, a.cookie)
	if resp.StatusCode != http.StatusFound || len(resp.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("redirect: %d, Set-Cookie %v", resp.StatusCode, resp.Header.Values("Set-Cookie"))
	}
}
