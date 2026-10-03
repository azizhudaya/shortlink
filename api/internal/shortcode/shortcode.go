// Package shortcode generates unguessable short codes. Never sequential:
// that would let anyone enumerate every link.
package shortcode

import (
	"crypto/rand"
	"math/big"
)

const (
	Length   = 7
	alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// Generate returns a 7-character base62 code from a CSPRNG. Collision
// handling (retry up to 5 times) lives in the store layer, which is
// the only place that knows whether a given code is already taken.
func Generate() (string, error) {
	b := make([]byte, Length)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b), nil
}
