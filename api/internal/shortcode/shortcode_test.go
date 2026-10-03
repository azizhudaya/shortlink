package shortcode

import (
	"strings"
	"testing"
)

func TestGenerateFormat(t *testing.T) {
	for i := 0; i < 100; i++ {
		code, err := Generate()
		if err != nil {
			t.Fatalf("Generate() error: %v", err)
		}
		if len(code) != Length {
			t.Fatalf("Generate() = %q, want length %d", code, Length)
		}
		for _, r := range code {
			if !strings.ContainsRune(alphabet, r) {
				t.Fatalf("Generate() = %q contains non-base62 rune %q", code, r)
			}
		}
	}
}

// Not a statistical test — just a smoke check that codes are not sequential
// or constant.
func TestGenerateIsNotSequential(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		code, err := Generate()
		if err != nil {
			t.Fatalf("Generate() error: %v", err)
		}
		if _, dup := seen[code]; dup {
			t.Fatalf("Generate() produced duplicate %q within 1000 draws", code)
		}
		seen[code] = struct{}{}
	}
}
