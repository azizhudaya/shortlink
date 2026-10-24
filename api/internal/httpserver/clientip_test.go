package httpserver

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, xff, remote, want string
	}{
		{name: "rightmost forwarded entry", xff: "1.1.1.1, 2.2.2.2", remote: "10.0.0.1:443", want: "2.2.2.2"},
		{name: "single forwarded entry", xff: "4.4.4.4", remote: "10.0.0.1:443", want: "4.4.4.4"},
		{name: "remote addr ipv4", remote: "3.3.3.3:5", want: "3.3.3.3"},
		{name: "remote addr ipv6", remote: "[::1]:5", want: "::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := clientIP(r); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
