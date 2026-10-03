package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Content-Type gate runs before validation or storage, so a zero Server
// is enough: any request that gets past it would panic on the nil store.
func TestCreateLinkRejectsNonJSON(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
	}{
		{name: "missing", contentType: ""},
		{name: "html form", contentType: "application/x-www-form-urlencoded"},
		{name: "multipart form", contentType: "multipart/form-data; boundary=x"},
		{name: "text/plain smuggling JSON", contentType: "text/plain"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(`{"url":"https://example.com"}`))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rec := httptest.NewRecorder()

			(&Server{}).CreateLink(rec, req)

			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
			}
		})
	}
}
