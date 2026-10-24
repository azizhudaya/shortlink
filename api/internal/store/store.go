package store

import (
	"context"
	"errors"
	"time"

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

	// ErrNotFound maps to 404. Ownership misses return it too, never a
	// distinct "forbidden" (FR-12).
	ErrNotFound = errors.New("not found")

	// ErrTakenDown maps to 409: the owner may not change or free a link an
	// operator or the system disabled (D6).
	ErrTakenDown = errors.New("link taken down")

	// ErrBadCursor maps to 400.
	ErrBadCursor = errors.New("invalid cursor")
)

// MaxGenerateAttempts bounds the retry loop for generated codes.
const MaxGenerateAttempts = 5

type Store interface {
	// CreateWithGeneratedCode retries on collision up to MaxGenerateAttempts
	// before returning ErrCodeExhausted. Both creates also append a
	// link_creations row in the same transaction (D24).
	CreateWithGeneratedCode(ctx context.Context, userID int64, longURL string) (model.Link, error)

	// CreateWithAlias inserts once. A collision is ErrAliasTaken, never a
	// silent substitution.
	CreateWithAlias(ctx context.Context, userID int64, alias, longURL string) (model.Link, error)

	// CreationQuota reports the user's creations in the last 24h, the oldest
	// one's time (zero if none), the account's age and D29's whitelist flag.
	CreationQuota(ctx context.Context, userID int64) (count int, oldest time.Time, accountCreated time.Time, whitelisted bool, err error)

	// ListByOwner pages newest first (D15). An empty cursor is the first
	// page; an empty nextCursor means there is no next page.
	ListByOwner(ctx context.Context, userID int64, cursor string, limit int) (links []model.Link, nextCursor string, err error)

	// SetDisabledByOwner is idempotent. ErrNotFound, ErrTakenDown.
	SetDisabledByOwner(ctx context.Context, userID int64, code string, disabled bool) error

	// DeleteByOwner soft-deletes (D31): the link stops working and disappears
	// from the owner's list, but its code stays reserved. ErrNotFound,
	// ErrTakenDown.
	DeleteByOwner(ctx context.Context, userID int64, code string) error

	// Takedown disables any link as operator:<actor> (D4). ErrNotFound.
	Takedown(ctx context.Context, code, actor, reason string) error

	// GetByCode is the hottest query in the system: a single primary-key
	// probe. Returns ErrNotFound for an unknown code.
	GetByCode(ctx context.Context, code string) (model.Link, error)

	// Ping reports database reachability for /healthz.
	Ping(ctx context.Context) error
}
