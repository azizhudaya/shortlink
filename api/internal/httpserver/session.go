package httpserver

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/auth"
	"github.com/azizhudaya/shortlink/api/internal/model"
	"github.com/azizhudaya/shortlink/api/internal/store"
)

const (
	sessionMaxAge = 30 * 24 * time.Hour // absolute, never extended (D7)
	oauthMaxAge   = 10 * time.Minute    // D9
)

// Cookie names. __Host- makes the browser refuse a Domain attribute, so the
// redirect apex afhs.my.id never receives either cookie (PRD §14).
func (s *Server) sessionCookieName() string { return s.cookiePrefix() + "sl_session" }
func (s *Server) oauthCookieName() string   { return s.cookiePrefix() + "sl_oauth" }

func (s *Server) cookiePrefix() string {
	if s.cfg.InsecureCookies {
		return "" // __Host- requires Secure, which http://localhost can't have
	}
	return "__Host-"
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(maxAge / time.Second),
		HttpOnly: true,
		Secure:   !s.cfg.InsecureCookies,
		// Lax, not Strict: the callback is a top-level GET arriving from
		// accounts.google.com and must carry the oauth cookie.
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: !s.cfg.InsecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

type sessionKey struct{}

type requestSession struct {
	model.Session
	token string // the raw cookie value, needed only to check CSRF
}

// sessionFrom returns the session requireSession attached.
func sessionFrom(ctx context.Context) model.Session {
	rs, _ := ctx.Value(sessionKey{}).(requestSession)
	return rs.Session
}

// requireSession resolves the session cookie or answers 401.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.sessionCookieName())
		if err != nil || c.Value == "" {
			writeError(w, http.StatusUnauthorized, CodeUnauthorized, "Sign in to continue.")
			return
		}
		sess, err := s.auth.GetSession(r.Context(), auth.HashToken(c.Value))
		if errors.Is(err, store.ErrSessionInvalid) {
			writeError(w, http.StatusUnauthorized, CodeUnauthorized, "Sign in to continue.")
			return
		}
		if err != nil {
			s.serverError(w, "loading session", err)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey{}, requestSession{Session: sess, token: c.Value})
		next(w, r.WithContext(ctx))
	}
}

// requireCSRF checks X-CSRF-Token against the session (D8). It must run
// inside requireSession.
func (s *Server) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rs, _ := r.Context().Value(sessionKey{}).(requestSession)
		if !auth.CSRFValid(rs.token, r.Header.Get("X-CSRF-Token")) {
			writeError(w, http.StatusForbidden, CodeCSRFInvalid, "Missing or invalid CSRF token. Reload the page and try again.")
			return
		}
		next(w, r)
	}
}

// writeRateLimited answers 429 with Retry-After in whole seconds.
func writeRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
	writeError(w, http.StatusTooManyRequests, CodeRateLimited, "Too many requests. Try again later.")
}
