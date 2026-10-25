package httpserver

import (
	"net/http"
	"strings"

	"github.com/azizhudaya/shortlink/api/internal/config"
)

// Home handles GET / — someone typed the bare redirect domain (FR-28). There
// is no page here, so send them to the app.
//
// 302, not 301: a permanent redirect is cached by browsers indefinitely,
// which would make a future landing page on this domain unreachable for
// anyone who had already visited.
func (s *Server) Home(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimRight(s.cfg.AppURL, "/")
	if target == "" {
		target = config.DefaultAppURL
	}
	http.Redirect(w, r, target+"/", http.StatusFound)
}
