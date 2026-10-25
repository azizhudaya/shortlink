package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/auth"
	"github.com/azizhudaya/shortlink/api/internal/config"
	"github.com/azizhudaya/shortlink/api/internal/ratelimit"
	"github.com/azizhudaya/shortlink/api/internal/safety"
	"github.com/azizhudaya/shortlink/api/internal/store"
	"github.com/azizhudaya/shortlink/api/internal/urlvalidate"
)

// ClickRecorder counts a served redirect (D32). It must not block: the
// redirect path stays a single database read.
type ClickRecorder interface {
	Record(code string)
}

type noClicks struct{}

func (noClicks) Record(string) {}

// IdentityProvider is the Google sign-in flow; tests run it against a fake.
type IdentityProvider interface {
	AuthURL(auth.FlowState) string
	Exchange(ctx context.Context, code string, fs auth.FlowState) (auth.Identity, error)
}

type Server struct {
	cfg       config.Config
	store     store.Store
	auth      store.AuthStore
	idp       IdentityProvider
	checker   safety.Checker
	clicks    ClickRecorder
	validator *urlvalidate.Validator
	logger    *slog.Logger

	// Burst limits (D12) and the daily quota (D24). Fields so tests can
	// override them.
	authPerIP      *ratelimit.Limiter
	createPerUser  *ratelimit.Limiter
	createPerIP    *ratelimit.Limiter
	newAccountDays int
	newAccountCap  int
	quotaCap       int
}

// rate-limiter key capacity: beyond it new keys are refused (fail closed).
const (
	limiterMaxKeys = 10_000
	limiterIdleTTL = time.Hour
)

func NewServer(cfg config.Config, st store.Store, as store.AuthStore, idp IdentityProvider, checker safety.Checker, clicks ClickRecorder, logger *slog.Logger) *Server {
	if clicks == nil {
		clicks = noClicks{}
	}
	return &Server{
		cfg:            cfg,
		store:          st,
		auth:           as,
		idp:            idp,
		checker:        checker,
		clicks:         clicks,
		validator:      urlvalidate.New(cfg.PublicHostnames),
		logger:         logger,
		authPerIP:      ratelimit.New(6*time.Second, 10, limiterIdleTTL, limiterMaxKeys),
		createPerUser:  ratelimit.New(time.Minute, 20, limiterIdleTTL, limiterMaxKeys),
		createPerIP:    ratelimit.New(30*time.Second, 30, limiterIdleTTL, limiterMaxKeys),
		newAccountDays: 7,
		newAccountCap:  10,
		quotaCap:       100,
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

	mux.HandleFunc("GET /{$}", s.Home) // exactly "/", never a short code
	mux.HandleFunc("GET /healthz", s.Health)
	mux.HandleFunc("GET /api/auth/google/start", s.GoogleStart)
	mux.HandleFunc("GET /api/auth/google/callback", s.GoogleCallback)
	mux.HandleFunc("POST /api/auth/logout", s.requireSession(s.requireCSRF(s.Logout)))
	mux.HandleFunc("GET /api/auth/me", s.requireSession(s.Me))
	mux.HandleFunc("POST /api/links", s.requireSession(s.requireCSRF(requireJSON(s.CreateLink))))
	mux.HandleFunc("GET /api/links", s.requireSession(s.ListLinks))
	mux.HandleFunc("PATCH /api/links/{code}", s.requireSession(s.requireCSRF(requireJSON(s.PatchLink))))
	mux.HandleFunc("DELETE /api/links/{code}", s.requireSession(s.requireCSRF(s.DeleteLink)))
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
