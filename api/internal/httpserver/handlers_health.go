package httpserver

import (
	"context"
	"net/http"
	"time"
)

type healthResponse struct {
	Status string `json:"status"` // "ok" | "degraded"
}

// Health reports process liveness and database reachability.
//
// Deliberately reports only ok/degraded with no version or dependency
// detail: it is publicly reachable so external
// monitoring can use it without credentials, which also means a scanner can
// reach it, so it must not confirm anything about the stack.
func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.store.Ping(ctx); err != nil {
		// Logged, not returned: the operator needs the reason, the caller
		// does not get to learn it.
		s.logger.Error("healthz database ping failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "degraded"})
		return
	}

	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}
