package store

import (
	"context"
	"errors"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/model"
)

var (
	// ErrSessionInvalid covers unknown, expired, and deactivated-account
	// sessions alike: all are a 401.
	ErrSessionInvalid = errors.New("session invalid")

	// ErrUserDisabled is a sign-in by a banned account. It is never recreated.
	ErrUserDisabled = errors.New("account disabled")

	// ErrSignupsClosed is a first sign-in while SIGNUPS_OPEN=false (D25).
	ErrSignupsClosed = errors.New("sign-ups closed")

	// ErrAmbiguousEmail is an operator command naming an email that more
	// than one account has (recycled addresses). Nothing is changed.
	ErrAmbiguousEmail = errors.New("more than one account has that email")
)

// RecentUser is one row of `user list-recent`.
type RecentUser struct {
	User  model.User
	Links int
}

type AuthStore interface {
	// Existing sub → refresh email + last_login_at (ErrUserDisabled if inactive).
	// Unknown sub → insert when allowNew, else ErrSignupsClosed.
	UpsertGoogleUser(ctx context.Context, sub, email string, allowNew bool) (u model.User, created bool, err error)
	CreateSession(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time, userAgent, ip string) error
	// Inactive account → ErrSessionInvalid; an expired row is deleted, then ErrSessionInvalid.
	GetSession(ctx context.Context, tokenHash []byte) (model.Session, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	// Sessions past expiry and link_creations rows older than 24h (D14).
	PurgeExpired(ctx context.Context) error
	// D26. One tx: is_active=false, delete sessions, take down every link not
	// already taken down. ErrNotFound, ErrAmbiguousEmail.
	BanUser(ctx context.Context, email, actor, reason string) (linksTakenDown int, sessionsDeleted int, err error)
	ListRecentUsers(ctx context.Context, since time.Time) ([]RecentUser, error)
	// D28. is_active=true; links stay taken down. ErrNotFound, ErrAmbiguousEmail.
	EnableUser(ctx context.Context, email string) (model.User, error)
	// D29. ErrNotFound, ErrAmbiguousEmail.
	SetWhitelisted(ctx context.Context, email string, on bool) (model.User, error)
}
