package alias

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{"minimum length", "abc", nil},
		{"maximum length", strings.Repeat("a", 32), nil},
		{"underscore and hyphen", "my_link-2", nil},
		{"mixed case", "MyCV", nil},

		{"too short", "ab", ErrTooShort},
		{"empty", "", ErrTooShort},
		{"too long", strings.Repeat("a", 33), ErrTooLong},
		{"space", "my link", ErrBadFormat},
		{"dot", "my.link", ErrBadFormat},
		{"slash", "my/link", ErrBadFormat},
		{"unicode", "café", ErrBadFormat},

		{"reserved api", "api", ErrIsReserved},
		{"reserved healthz", "healthz", ErrIsReserved},
		// Reserved for future routes.
		{"reserved login", "login", ErrIsReserved},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.input)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Validate(%q) = %v, want nil", tc.input, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate(%q) = %v, want %v", tc.input, err, tc.want)
			}
		})
	}
}

// Every literal route the router registers must be reserved, or a user could
// claim an alias permanently shadowed by that route.
func TestRoutePathsAreReserved(t *testing.T) {
	for _, path := range []string{"healthz", "api"} {
		if !IsReserved(path) {
			t.Errorf("route path %q is registered in the router but not reserved", path)
		}
	}
}
