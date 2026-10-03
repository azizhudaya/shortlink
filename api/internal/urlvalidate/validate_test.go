package urlvalidate

import (
	"errors"
	"net"
	"testing"
)

// newTestValidator stubs DNS so the private-address branch is exercised
// without depending on real resolution.
func newTestValidator(resolved map[string][]string) *Validator {
	v := New([]string{"afh.my.id", "app.afh.my.id"})
	v.resolve = func(host string) ([]net.IP, error) {
		addrs, ok := resolved[host]
		if !ok {
			return nil, errors.New("no such host")
		}
		ips := make([]net.IP, 0, len(addrs))
		for _, a := range addrs {
			ips = append(ips, net.ParseIP(a))
		}
		return ips, nil
	}
	return v
}

func TestValidate(t *testing.T) {
	v := newTestValidator(map[string][]string{
		"example.com": {"93.184.216.34"},
		// A host with one public and one loopback record must be rejected:
		// the check is "any non-public address", not "all".
		"mixed.example":    {"93.184.216.34", "127.0.0.1"},
		"internal.example": {"10.0.0.5"},
	})

	tests := []struct {
		name    string
		input   string
		want    error
		wantURL string
	}{
		{name: "https URL", input: "https://example.com/path?a=b", wantURL: "https://example.com/path?a=b"},
		{name: "http URL", input: "http://example.com", wantURL: "http://example.com"},
		// Scheme-less input gets https:// assumed.
		{name: "no scheme", input: "example.com/page", wantURL: "https://example.com/page"},
		// A bare host:port parses as scheme "example.com" — it must still be
		// treated as a host needing a scheme, not as a forbidden scheme.
		{name: "no scheme with port", input: "example.com:8080/path", wantURL: "https://example.com:8080/path"},
		{name: "protocol relative", input: "//example.com/page", wantURL: "https://example.com/page"},
		{name: "surrounding whitespace", input: "  example.com/page  ", wantURL: "https://example.com/page"},

		{name: "javascript scheme", input: "javascript:alert(1)", want: ErrBadScheme},
		{name: "data scheme", input: "data:text/html,<script>", want: ErrBadScheme},
		{name: "file scheme", input: "file:///etc/passwd", want: ErrBadScheme},

		// Loop prevention.
		{name: "own apex", input: "https://afh.my.id/abc", want: ErrOwnDomain},
		{name: "own app host", input: "https://app.afh.my.id/", want: ErrOwnDomain},
		{name: "own host mixed case", input: "https://AFH.MY.ID/abc", want: ErrOwnDomain},

		// SSRF / internal-target rejection.
		{name: "loopback literal", input: "http://127.0.0.1:8080", want: ErrPrivateAddr},
		{name: "private literal", input: "http://192.168.1.1", want: ErrPrivateAddr},
		{name: "link-local literal", input: "http://169.254.169.254/latest/meta-data", want: ErrPrivateAddr},
		{name: "ipv6 loopback", input: "http://[::1]/", want: ErrPrivateAddr},
		{name: "cgnat literal", input: "http://100.64.0.1", want: ErrPrivateAddr},
		{name: "resolves to private", input: "https://internal.example", want: ErrPrivateAddr},
		{name: "one record private", input: "https://mixed.example", want: ErrPrivateAddr},

		{name: "unresolvable", input: "https://nonexistent.invalid", want: ErrUnresolvable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := v.Validate(tc.input)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("Validate(%q) error = %v, want %v", tc.input, err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate(%q) = %v, want nil", tc.input, err)
			}
			if got != tc.wantURL {
				t.Fatalf("Validate(%q) = %q, want %q", tc.input, got, tc.wantURL)
			}
		})
	}
}
