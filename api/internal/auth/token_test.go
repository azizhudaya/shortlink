package auth

import (
	"bytes"
	"regexp"
	"testing"
)

var base64url43 = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func TestNewSessionToken(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		token, hash, err := NewSessionToken()
		if err != nil {
			t.Fatal(err)
		}
		if !base64url43.MatchString(token) {
			t.Fatalf("token %q: want 43 URL-safe chars", token)
		}
		if seen[token] {
			t.Fatalf("token %q repeated", token)
		}
		seen[token] = true
		if len(hash) != 32 || !bytes.Equal(hash, HashToken(token)) {
			t.Fatalf("hash = %x, want SHA-256 of the token (%x)", hash, HashToken(token))
		}
	}
}

func TestCSRF(t *testing.T) {
	session, _, _ := NewSessionToken()
	other, _, _ := NewSessionToken()
	csrf := CSRFToken(session)

	if !CSRFValid(session, csrf) {
		t.Error("CSRF token for its own session rejected")
	}
	if CSRFValid(other, csrf) {
		t.Error("CSRF token accepted for another session")
	}
	if CSRFValid(session, "") {
		t.Error("empty CSRF token accepted")
	}
	if CSRFValid("", CSRFToken("")) {
		t.Error("CSRF accepted without a session")
	}
}

func TestFlowStateRoundTrip(t *testing.T) {
	fs, err := NewFlowState()
	if err != nil {
		t.Fatal(err)
	}
	if !base64url43.MatchString(fs.State) || !base64url43.MatchString(fs.Nonce) || !base64url43.MatchString(fs.Verifier) {
		t.Fatalf("flow state parts not 32-byte base64url: %+v", fs)
	}
	other, _ := NewFlowState()
	if fs.State == other.State || fs.Nonce == other.Nonce || fs.Verifier == other.Verifier {
		t.Fatal("two flow states share a value")
	}

	got, err := DecodeFlowState(fs.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if got != fs {
		t.Fatalf("round trip = %+v, want %+v", got, fs)
	}
}

func TestDecodeFlowStateRejects(t *testing.T) {
	for _, s := range []string{"", "a.b", "a.b.c.d", "a+b.c.d", "a..c", "a.b/c.d"} {
		if _, err := DecodeFlowState(s); err == nil {
			t.Errorf("DecodeFlowState(%q) accepted", s)
		}
	}
}
