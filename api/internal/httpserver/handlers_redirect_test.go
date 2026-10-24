package httpserver

import (
	"fmt"
	"net/http"
	"testing"
)

func TestRedirectRecordsClickOnlyOn302(t *testing.T) {
	e := newEnv(t)
	a := e.account(t, "a")
	e.mustCreate(t, a, `{"url":"https://example.com","custom_alias":"mine"}`)

	if got := e.redirectStatus(t, "mine"); got != http.StatusFound {
		t.Fatalf("redirect: %d; server log:\n%s", got, e.logs.String())
	}
	e.redirectStatus(t, "nosuch") // 404
	e.request(t, "HEAD", "/mine", "", nil)
	_ = e.send(t, a, "PATCH", "/api/links/mine", `{"disabled":true}`)
	e.redirectStatus(t, "mine") // 410

	if got := fmt.Sprint(e.clicks.recorded()); got != "[mine]" {
		t.Fatalf("recorded clicks = %s, want [mine] (one GET that redirected)", got)
	}
}
