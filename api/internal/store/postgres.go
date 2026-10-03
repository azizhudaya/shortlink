package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/azizhudaya/shortlink/api/internal/model"
	"github.com/azizhudaya/shortlink/api/internal/shortcode"
)

// uniqueViolation is Postgres error code 23505 — the primary-key conflict we
// rely on for collision safety. Letting the database's unique constraint be
// the arbiter means no application-level locking.
const uniqueViolation = "23505"

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

const insertSQL = `
	INSERT INTO links (short_code, long_url, is_custom)
	VALUES ($1, $2, $3)
	RETURNING short_code, long_url, is_custom, user_id,
	          disabled_at, disabled_by, disable_reason, created_at`

const selectSQL = `
	SELECT short_code, long_url, is_custom, user_id,
	       disabled_at, disabled_by, disable_reason, created_at
	FROM links
	WHERE short_code = $1`

func (p *Postgres) CreateWithGeneratedCode(ctx context.Context, longURL string) (model.Link, error) {
	for attempt := 0; attempt < MaxGenerateAttempts; attempt++ {
		code, err := shortcode.Generate()
		if err != nil {
			return model.Link{}, fmt.Errorf("generating code: %w", err)
		}

		link, err := p.insert(ctx, code, longURL, false)
		if err == nil {
			return link, nil
		}
		if isUniqueViolation(err) {
			continue // collision: try a different code
		}
		return model.Link{}, err
	}
	return model.Link{}, ErrCodeExhausted
}

func (p *Postgres) CreateWithAlias(ctx context.Context, aliasStr, longURL string) (model.Link, error) {
	link, err := p.insert(ctx, aliasStr, longURL, true)
	if isUniqueViolation(err) {
		// No retry, no substitute code — the caller asked for this exact
		// alias and must be told it is unavailable.
		return model.Link{}, ErrAliasTaken
	}
	return link, err
}

func (p *Postgres) insert(ctx context.Context, code, longURL string, isCustom bool) (model.Link, error) {
	row := p.pool.QueryRow(ctx, insertSQL, code, longURL, isCustom)
	return scanLink(row)
}

func (p *Postgres) GetByCode(ctx context.Context, code string) (model.Link, error) {
	row := p.pool.QueryRow(ctx, selectSQL, code)
	link, err := scanLink(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Link{}, ErrNotFound
	}
	return link, err
}

func (p *Postgres) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

type scannable interface {
	Scan(dest ...any) error
}

func scanLink(row scannable) (model.Link, error) {
	var l model.Link
	err := row.Scan(
		&l.ShortCode, &l.LongURL, &l.IsCustom, &l.UserID,
		&l.DisabledAt, &l.DisabledBy, &l.DisableReason, &l.CreatedAt,
	)
	return l, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
