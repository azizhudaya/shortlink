package httpserver

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/auth"
	"github.com/azizhudaya/shortlink/api/internal/store"
)

// GoogleStart handles GET /api/auth/google/start: remember state, nonce and
// PKCE verifier in a short-lived cookie, then send the browser to Google.
func (s *Server) GoogleStart(w http.ResponseWriter, r *http.Request) {
	if ok, retry := s.authPerIP.Allow(clientIP(r)); !ok {
		writeRateLimited(w, retry)
		return
	}
	fs, err := auth.NewFlowState()
	if err != nil {
		s.serverError(w, "creating oauth flow state", err)
		return
	}
	s.setCookie(w, s.oauthCookieName(), fs.Encode(), oauthMaxAge)
	http.Redirect(w, r, s.idp.AuthURL(fs), http.StatusFound)
}

// Callback failure codes (D11). The cause goes to the log, never the URL.
const (
	signinFailed          = "failed"
	signinSignupsClosed   = "signups_closed"
	signinEmailUnverified = "email_unverified"
	signinAccountDisabled = "account_disabled"
)

// GoogleCallback handles GET /api/auth/google/callback. It is a browser
// navigation, so every outcome is a redirect into the UI.
func (s *Server) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	if ok, retry := s.authPerIP.Allow(clientIP(r)); !ok {
		writeRateLimited(w, retry)
		return
	}

	// The flow state is single-use: clear it whatever happens next.
	c, cookieErr := r.Cookie(s.oauthCookieName())
	s.clearCookie(w, s.oauthCookieName())

	q := r.URL.Query()
	if q.Get("error") != "" {
		// The user cancelled at Google (or Google refused): not our failure.
		http.Redirect(w, r, "/signin", http.StatusFound)
		return
	}

	fail := func(code, reason string, err error) {
		s.logger.Warn("sign-in failed", "reason", reason, "error", err)
		http.Redirect(w, r, "/signin?error="+code, http.StatusFound)
	}

	if cookieErr != nil {
		fail(signinFailed, "no oauth cookie", nil)
		return
	}
	fs, err := auth.DecodeFlowState(c.Value)
	if err != nil {
		fail(signinFailed, "bad oauth cookie", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(fs.State)) != 1 {
		fail(signinFailed, "state mismatch", nil)
		return
	}

	id, err := s.idp.Exchange(r.Context(), q.Get("code"), fs)
	if errors.Is(err, auth.ErrEmailUnverified) {
		fail(signinEmailUnverified, "email not verified", nil)
		return
	}
	if err != nil {
		fail(signinFailed, "token exchange", err)
		return
	}

	u, created, err := s.auth.UpsertGoogleUser(r.Context(), id.Sub, id.Email, s.cfg.SignupsOpen)
	switch {
	case errors.Is(err, store.ErrSignupsClosed):
		fail(signinSignupsClosed, "sign-ups closed", nil)
		return
	case errors.Is(err, store.ErrUserDisabled):
		fail(signinAccountDisabled, "account disabled", nil)
		return
	case err != nil:
		fail(signinFailed, "upserting user", err)
		return
	}

	token, hash, err := auth.NewSessionToken()
	if err != nil {
		fail(signinFailed, "creating session token", err)
		return
	}
	if err := s.auth.CreateSession(r.Context(), hash, u.ID, time.Now().Add(sessionMaxAge), r.UserAgent(), clientIP(r)); err != nil {
		fail(signinFailed, "storing session", err)
		return
	}
	s.setCookie(w, s.sessionCookieName(), token, sessionMaxAge)
	s.logger.Info("sign-in", "user_id", u.ID, "created", created)
	http.Redirect(w, r, "/links", http.StatusFound)
}

// Logout handles POST /api/auth/logout.
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	rs, _ := r.Context().Value(sessionKey{}).(requestSession)
	if err := s.auth.DeleteSession(r.Context(), auth.HashToken(rs.token)); err != nil {
		s.serverError(w, "deleting session", err)
		return
	}
	s.clearCookie(w, s.sessionCookieName())
	w.WriteHeader(http.StatusNoContent)
}

type meResponse struct {
	Email     string `json:"email"`
	CSRFToken string `json:"csrf_token"`
}

// Me handles GET /api/auth/me: who is signed in, and the CSRF token the UI
// must echo on every mutating request.
func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	rs, _ := r.Context().Value(sessionKey{}).(requestSession)
	writeJSON(w, http.StatusOK, meResponse{Email: rs.Email, CSRFToken: auth.CSRFToken(rs.token)})
}
