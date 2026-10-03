package store

import (
	"context"
	"errors"

	"github.com/azizhudaya/shortlink/api/internal/model"
)

var (
	// ErrAliasTaken maps to 409. Returned only for custom aliases — the
	// caller must NOT substitute a generated code.
	ErrAliasTaken = errors.New("alias already in use")

	// ErrCodeExhausted maps to 500. Returned when generated-code insertion
	// collided MaxGenerateAttempts times, which at 62^7 codes means
	// something is wrong beyond bad luck.
	ErrCodeExhausted = errors.New("could not generate an unused code")

	// ErrNotFound maps to 404 on the redirect path.
	ErrNotFound = errors.New("link not found")
)

// MaxGenerateAttempts bounds the retry loop for generated codes.
const MaxGenerateAttempts = 5

type Store interface {
	// CreateWithGeneratedCode retries on collision up to MaxGenerateAttempts
	// before returning ErrCodeExhausted.
	CreateWithGeneratedCode(ctx context.Context, longURL string) (model.Link, error)

	// CreateWithAlias inserts once. A collision is ErrAliasTaken, never a
	// silent substitution.
	CreateWithAlias(ctx context.Context, alias, longURL string) (model.Link, error)

	// GetByCode is the hottest query in the system: a single primary-key
	// probe. Returns ErrNotFound for an unknown code.
	GetByCode(ctx context.Context, code string) (model.Link, error)

	// Ping reports database reachability for /healthz.
	Ping(ctx context.Context) error
}
