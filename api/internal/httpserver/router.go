package httpserver

import (
	"log/slog"
	"net/http"

	"github.com/azizhudaya/shortlink/api/internal/config"
	"github.com/azizhudaya/shortlink/api/internal/store"
	"github.com/azizhudaya/shortlink/api/internal/urlvalidate"
)

type Server struct {
	cfg       config.Config
	store     store.Store
	validator *urlvalidate.Validator
	logger    *slog.Logger
}

func NewServer(cfg config.Config, st store.Store, logger *slog.Logger) *Server {
	return &Server{
		cfg:       cfg,
		store:     st,
		validator: urlvalidate.New(cfg.PublicHostnames),
		logger:    logger,
	}
}

// Routes registers every path this service serves.
//
// One mux serves both hostnames: Caddy routes afh.my.id here for redirects
// and app.afh.my.id/api/* here for the API, so the API is same-origin
// with the UI and CORS never enters the system.
//
// Go 1.22's ServeMux prefers a literal pattern over an overlapping wildcard,
// so `GET /healthz` wins over `GET /{code}` for that exact path. Every
// literal registered here must also appear in alias.Reserved, or a user
// could claim an alias that is permanently shadowed by a real route.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.Health)
	mux.HandleFunc("POST /api/links", s.CreateLink)
	mux.HandleFunc("GET /{code}", s.Redirect)

	return Chain(mux,
		Recover,
		SecurityHeaders,
		AccessLog,
		LimitBody,
	)
}

// shortURL builds the public form of a code. The first configured hostname
// is the redirect hostname (afh.my.id), not the app hostname.
func (s *Server) shortURL(code string) string {
	host := "afh.my.id"
	if len(s.cfg.PublicHostnames) > 0 {
		host = s.cfg.PublicHostnames[0]
	}
	return "https://" + host + "/" + code
}

// serverError logs the real cause and returns a generic message — internal
// error detail is for the operator's logs, not the response body.
func (s *Server) serverError(w http.ResponseWriter, context string, err error) {
	s.logger.Error(context, "error", err)
	writeError(w, http.StatusInternalServerError, CodeInternalError, "Something went wrong.")
}
