package httpserver

import (
	"log/slog"
	"net/http"
	"time"
)

type Middleware func(http.Handler) http.Handler

// Chain applies middleware so the first argument is the outermost wrapper.
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// SecurityHeaders sets the headers required on every response.
//
// Referrer-Policy matters more here than in a typical service: without it,
// every destination site learns the short URL that sent the visitor, which
// would make the link inventory partially readable by third parties.
// HSTS is also set by Caddy; setting it here too means it holds even if the
// proxy config regresses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the status code for logging without buffering the
// body — the redirect path must stay a single write.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// AccessLog emits one structured JSON line per request to stdout, retained by
// `docker logs` with a size-capped rotating driver.
//
// Deliberately does NOT log visitor identifiers on the redirect path:
// no IP, no user agent, no cookies.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// Recover converts a panic into a 500 rather than a dropped connection, so
// one bad request cannot take the process down and with it every redirect.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				slog.Error("panic recovered", "value", v, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "INTERNAL", "Something went wrong.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// MaxBodyBytes caps request bodies at 1 MB. Caddy also caps this;
// this is the in-application backstop.
const MaxBodyBytes = 1 << 20

func LimitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}
