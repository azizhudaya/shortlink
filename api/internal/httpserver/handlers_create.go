package httpserver

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/alias"
	"github.com/azizhudaya/shortlink/api/internal/model"
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

// CreateLink handles POST /api/links.
//
// NOTE: this endpoint is UNAUTHENTICATED until accounts are added. Do not
// expose it to the public internet without an interim access restriction.
// In M1 that restriction is Caddy basic auth on app.afh.my.id (afh-infra).
func (s *Server) CreateLink(w http.ResponseWriter, r *http.Request) {
	// Browsers attach cached basic-auth credentials to cross-site requests
	// too. A plain HTML form cannot send application/json, and a cross-site
	// fetch that does triggers a CORS preflight this API never answers — so
	// requiring JSON stops other sites from creating links as the operator.
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Content-Type must be application/json.")
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

	var link model.Link
	if req.CustomAlias != "" {
		if err := alias.Validate(req.CustomAlias); err != nil {
			writeError(w, http.StatusBadRequest, CodeInvalidAlias, err.Error())
			return
		}
		link, err = s.store.CreateWithAlias(r.Context(), req.CustomAlias, longURL)
		if errors.Is(err, store.ErrAliasTaken) {
			// 409 with no substitute code.
			writeError(w, http.StatusConflict, CodeAliasTaken,
				"That alias is already in use. Choose a different one.")
			return
		}
	} else {
		link, err = s.store.CreateWithGeneratedCode(r.Context(), longURL)
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
