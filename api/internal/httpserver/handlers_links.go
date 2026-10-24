package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/store"
)

type linkItem struct {
	Code      string    `json:"code"`
	ShortURL  string    `json:"short_url"`
	LongURL   string    `json:"long_url"`
	IsCustom  bool      `json:"is_custom"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
}

type listResponse struct {
	Links      []linkItem `json:"links"`
	NextCursor *string    `json:"next_cursor"`
}

const (
	defaultListLimit = 50
	maxListLimit     = 100
)

// ListLinks handles GET /api/links?cursor=&limit= (session): the caller's
// own links, newest first (D15).
func (s *Server) ListLinks(w http.ResponseWriter, r *http.Request) {
	limit := defaultListLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxListLimit {
			writeError(w, http.StatusBadRequest, CodeBadRequest, "limit must be between 1 and 100.")
			return
		}
		limit = n
	}

	links, next, err := s.store.ListByOwner(r.Context(), sessionFrom(r.Context()).UserID, r.URL.Query().Get("cursor"), limit)
	if errors.Is(err, store.ErrBadCursor) {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Invalid cursor.")
		return
	}
	if err != nil {
		s.serverError(w, "listing links", err)
		return
	}

	resp := listResponse{Links: make([]linkItem, 0, len(links))}
	for _, l := range links {
		resp.Links = append(resp.Links, linkItem{
			Code:      l.ShortCode,
			ShortURL:  s.shortURL(l.ShortCode),
			LongURL:   l.LongURL,
			IsCustom:  l.IsCustom,
			CreatedAt: l.CreatedAt,
			Status:    l.Status(),
		})
	}
	if next != "" {
		resp.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, resp)
}

type patchRequest struct {
	Disabled *bool `json:"disabled"`
}

// PatchLink handles PATCH /api/links/{code} (session → CSRF → JSON):
// {"disabled": bool}.
func (s *Server) PatchLink(w http.ResponseWriter, r *http.Request) {
	var req patchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Disabled == nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, `Body must be {"disabled": true|false}.`)
		return
	}
	err := s.store.SetDisabledByOwner(r.Context(), sessionFrom(r.Context()).UserID, r.PathValue("code"), *req.Disabled)
	s.writeOwnerResult(w, "updating link", err)
}

// DeleteLink handles DELETE /api/links/{code} (session → CSRF). The code
// becomes available to anyone.
func (s *Server) DeleteLink(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteByOwner(r.Context(), sessionFrom(r.Context()).UserID, r.PathValue("code"))
	s.writeOwnerResult(w, "deleting link", err)
}

// writeOwnerResult maps an owner action's outcome. Someone else's link is a
// 404, never a 403, so codes can't be probed for existence (FR-12).
func (s *Server) writeOwnerResult(w http.ResponseWriter, context string, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeNotFound, "Link not found.")
	case errors.Is(err, store.ErrTakenDown):
		writeError(w, http.StatusConflict, CodeLinkTakenDown,
			"This link was disabled by the operator and can't be changed.")
	default:
		s.serverError(w, context, err)
	}
}
