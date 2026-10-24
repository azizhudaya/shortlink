package httpserver

import (
	"net"
	"net/http"
	"strings"
)

// clientIP is the address rate limits key on (D13). Caddy is the only peer
// and appends the address it saw, so the rightmost X-Forwarded-For entry is
// the one a client cannot forge. Without the header (local runs), it is the
// TCP peer.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
