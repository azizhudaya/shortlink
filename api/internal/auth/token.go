// Package auth holds the pieces of sign-in that are pure computation: session
// and CSRF tokens, the OAuth flow-state cookie codec, and the Google OIDC
// client.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/oauth2"
)

var b64 = base64.RawURLEncoding

// NewSessionToken returns a random session token for the cookie and its
// SHA-256 for the database (D7). The database never holds a usable token.
func NewSessionToken() (token string, hash []byte, err error) {
	token, err = randomString()
	if err != nil {
		return "", nil, err
	}
	return token, HashToken(token), nil
}

// HashToken is the database key for a session token.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CSRFToken derives the per-session CSRF token (D8). It needs no storage: only
// someone holding the session cookie can compute it, and a cross-origin page
// can neither read the cookie nor the /api/auth/me response that carries it.
func CSRFToken(sessionToken string) string {
	sum := sha256.Sum256([]byte("csrf:" + sessionToken))
	return b64.EncodeToString(sum[:])
}

// CSRFValid compares in constant time. Without a session there is nothing to
// protect, and nothing is valid.
func CSRFValid(sessionToken, presented string) bool {
	if sessionToken == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(CSRFToken(sessionToken)), []byte(presented)) == 1
}

// FlowState is everything the callback must check against what the start
// endpoint sent to Google (D9). It lives in a short-lived cookie, not the DB.
type FlowState struct {
	State, Nonce, Verifier string
}

func NewFlowState() (FlowState, error) {
	state, err := randomString()
	if err != nil {
		return FlowState{}, err
	}
	nonce, err := randomString()
	if err != nil {
		return FlowState{}, err
	}
	return FlowState{State: state, Nonce: nonce, Verifier: oauth2.GenerateVerifier()}, nil
}

// Encode joins the parts with dots; base64url never contains one.
func (fs FlowState) Encode() string {
	return fs.State + "." + fs.Nonce + "." + fs.Verifier
}

var ErrBadFlowState = errors.New("malformed oauth flow state")

func DecodeFlowState(s string) (FlowState, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return FlowState{}, ErrBadFlowState
	}
	for _, p := range parts {
		if p == "" || strings.Trim(p, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
			return FlowState{}, ErrBadFlowState
		}
	}
	return FlowState{State: parts[0], Nonce: parts[1], Verifier: parts[2]}, nil
}

// randomString is 32 bytes from the OS CSPRNG, base64url without padding.
func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b64.EncodeToString(b), nil
}
