package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/azizhudaya/shortlink/api/internal/config"
)

// Typing the bare redirect domain lands on the app (FR-28). The handler
// touches no store, so a server without one is enough.
func TestHomeRedirectsToApp(t *testing.T) {
	cases := []struct {
		name, appURL, want string
	}{
		{name: "configured", appURL: "https://app.example.test", want: "https://app.example.test/"},
		{name: "default", appURL: "", want: "https://app.afhs.my.id/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer(config.Config{AppURL: tc.appURL}, nil, nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				rec := httptest.NewRecorder()
				s.Routes().ServeHTTP(rec, httptest.NewRequest(method, "/", nil))
				if rec.Code != http.StatusFound || rec.Header().Get("Location") != tc.want {
					t.Fatalf("%s /: %d → %q, want 302 → %q", method, rec.Code, rec.Header().Get("Location"), tc.want)
				}
				if len(rec.Header().Values("Set-Cookie")) != 0 {
					t.Fatalf("%s /: sets a cookie", method)
				}
			}
		})
	}
}
