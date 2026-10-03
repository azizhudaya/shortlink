package httpserver

import (
	"errors"
	"net/http"

	"github.com/azizhudaya/shortlink/api/internal/store"
)

// notFoundPage is a minimal branded page: enough to reassure a visitor who
// mistyped a printed code, with no service detail and no link back to the
// app. No JavaScript on the redirect path.
const notFoundPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Link not found</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:20vh auto;padding:0 1.5rem;line-height:1.6;color:#111}@media(prefers-color-scheme:dark){body{background:#111;color:#eee}}</style>
</head><body><h1>Link not found</h1>
<p>This short link doesn't exist. If you typed it by hand, check for a typo &mdash; short codes are case-sensitive.</p>
</body></html>
`

const gonePage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Link disabled</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:20vh auto;padding:0 1.5rem;line-height:1.6;color:#111}@media(prefers-color-scheme:dark){body{background:#111;color:#eee}}</style>
</head><body><h1>Link disabled</h1>
<p>This short link is no longer active.</p>
</body></html>
`

// Redirect serves the hottest path: one primary-key read and one 302.
//
// 302, not 301: browsers cache 301 aggressively and often indefinitely,
// which would make a takedown unenforceable for anyone who has already
// followed the link.
//
// Code matching is case-sensitive by virtue of the SQL comparison, so
// `/Abc123` and `/abc123` are distinct links.
func (s *Server) Redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")

	link, err := s.store.GetByCode(r.Context(), code)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeHTML(w, http.StatusNotFound, notFoundPage)
		return
	case err != nil:
		s.serverError(w, "redirect lookup failed", err)
		return
	}

	if link.IsDisabled() {
		writeHTML(w, http.StatusGone, gonePage)
		return
	}

	http.Redirect(w, r, link.LongURL, http.StatusFound)
}

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
