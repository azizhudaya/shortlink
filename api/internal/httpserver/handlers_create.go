package httpserver

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/alias"
	"github.com/azizhudaya/shortlink/api/internal/model"
	"github.com/azizhudaya/shortlink/api/internal/safety"
	"github.com/azizhudaya/shortlink/api/internal/store"
)

type createRequest struct {
	URL         string `json:"url"`
	CustomAlias string `json:"custom_alias"`
}

type createResponse struct {
	Code      string    `json:"code"`
	ShortURL  string    `json:"short_url"`
	LongURL   string    `json:"long_url"`
	IsCustom  bool      `json:"is_custom"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateLink handles POST /api/links (session → CSRF → JSON).
//
// Checks run cheapest rejection first, and Google is never asked about a
// request that would fail anyway: rate limits, the daily quota, URL and alias
// validation, then Safe Browsing, then the insert.
func (s *Server) CreateLink(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	userKey := strconv.FormatInt(sess.UserID, 10)
	if ok, retry := s.createPerUser.Allow(userKey); !ok {
		writeRateLimited(w, retry)
		return
	}
	if ok, retry := s.createPerIP.Allow(clientIP(r)); !ok {
		writeRateLimited(w, retry)
		return
	}
	if !s.withinQuota(w, r, sess.UserID) {
		return
	}

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Request body must be valid JSON.")
		return
	}

	// Destination validation first: an invalid URL should fail the same way
	// whether or not an alias was supplied.
	longURL, err := s.validator.Validate(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidURL, err.Error())
		return
	}
	if req.CustomAlias != "" {
		if err := alias.Validate(req.CustomAlias); err != nil {
			writeError(w, http.StatusBadRequest, CodeInvalidAlias, err.Error())
			return
		}
	}

	switch err := s.checker.Check(r.Context(), longURL); {
	case errors.Is(err, safety.ErrUnsafe):
		s.logger.Warn("unsafe destination refused", "user_id", sess.UserID, "host", hostOf(longURL))
		writeError(w, http.StatusBadRequest, CodeUnsafeURL,
			"This destination is flagged as unsafe by Google Safe Browsing.")
		return
	case err != nil:
		// Fail closed: no check, no link (D23).
		s.logger.Error("safety check unavailable", "error", err)
		writeError(w, http.StatusServiceUnavailable, CodeSafetyUnavailable,
			"We couldn't verify this link's safety right now. Try again in a minute.")
		return
	}

	var link model.Link
	if req.CustomAlias != "" {
		link, err = s.store.CreateWithAlias(r.Context(), sess.UserID, req.CustomAlias, longURL)
		if errors.Is(err, store.ErrAliasTaken) {
			// 409 with no substitute code.
			writeError(w, http.StatusConflict, CodeAliasTaken,
				"That alias is already in use. Choose a different one.")
			return
		}
	} else {
		link, err = s.store.CreateWithGeneratedCode(r.Context(), sess.UserID, longURL)
	}
	if err != nil {
		s.serverError(w, "creating link", err)
		return
	}

	writeJSON(w, http.StatusCreated, createResponse{
		Code:      link.ShortCode,
		ShortURL:  s.shortURL(link.ShortCode),
		LongURL:   link.LongURL,
		IsCustom:  link.IsCustom,
		CreatedAt: link.CreatedAt,
	})
}

// withinQuota enforces the D24 daily cap, which whitelisted users skip (D29).
// It writes the 429 itself and reports false when the user is at the cap.
func (s *Server) withinQuota(w http.ResponseWriter, r *http.Request, userID int64) bool {
	count, oldest, accountCreated, whitelisted, err := s.store.CreationQuota(r.Context(), userID)
	if err != nil {
		s.serverError(w, "reading creation quota", err)
		return false
	}
	if whitelisted {
		return true
	}
	limit := s.quotaCap
	if time.Since(accountCreated) < time.Duration(s.newAccountDays)*24*time.Hour {
		limit = s.newAccountCap
	}
	if count < limit {
		return true
	}
	// The cap frees up when the oldest creation in the window ages out.
	wait := time.Until(oldest.Add(24 * time.Hour))
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
	writeError(w, http.StatusTooManyRequests, CodeQuotaExceeded,
		"You've reached today's link limit.")
	return false
}

// hostOf is what a log may say about a flagged destination: never the path
// or query, which can carry the victim-specific part of a phishing URL.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
