package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

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

const linkCols = `short_code, long_url, is_custom, user_id,
	          disabled_at, disabled_by, disable_reason, created_at, deleted_at`

const insertSQL = `
	INSERT INTO links (short_code, long_url, is_custom, user_id)
	VALUES ($1, $2, $3, $4)
	RETURNING ` + linkCols

// A soft-deleted link is gone for every reader: the redirect answers 404.
// Its row keeps the code reserved (D31).
const selectSQL = `
	SELECT ` + linkCols + `
	FROM links
	WHERE short_code = $1 AND deleted_at IS NULL`

func (p *Postgres) CreateWithGeneratedCode(ctx context.Context, userID int64, longURL string) (model.Link, error) {
	for attempt := 0; attempt < MaxGenerateAttempts; attempt++ {
		code, err := shortcode.Generate()
		if err != nil {
			return model.Link{}, fmt.Errorf("generating code: %w", err)
		}

		link, err := p.insert(ctx, userID, code, longURL, false)
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

func (p *Postgres) CreateWithAlias(ctx context.Context, userID int64, aliasStr, longURL string) (model.Link, error) {
	link, err := p.insert(ctx, userID, aliasStr, longURL, true)
	if isUniqueViolation(err) {
		// No retry, no substitute code — the caller asked for this exact
		// alias and must be told it is unavailable.
		return model.Link{}, ErrAliasTaken
	}
	return link, err
}

// insert writes the link and its quota ledger row in one transaction, so a
// failed insert (a collision) consumes no quota.
func (p *Postgres) insert(ctx context.Context, userID int64, code, longURL string, isCustom bool) (link model.Link, err error) {
	err = pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		link, err = scanLink(tx.QueryRow(ctx, insertSQL, code, longURL, isCustom, userID))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO link_creations (user_id) VALUES ($1)`, userID)
		return err
	})
	return link, err
}

func (p *Postgres) CreationQuota(ctx context.Context, userID int64) (count int, oldest time.Time, accountCreated time.Time, whitelisted bool, err error) {
	var oldestPtr *time.Time
	err = p.pool.QueryRow(ctx, `
		SELECT u.created_at, u.is_whitelisted, count(c.user_id), min(c.created_at)
		FROM users u
		LEFT JOIN link_creations c
		       ON c.user_id = u.id AND c.created_at > now() - interval '24 hours'
		WHERE u.id = $1
		GROUP BY u.id`, userID).Scan(&accountCreated, &whitelisted, &count, &oldestPtr)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if oldestPtr != nil {
		oldest = *oldestPtr
	}
	return count, oldest, accountCreated, whitelisted, err
}

func (p *Postgres) ListByOwner(ctx context.Context, userID int64, cursor string, limit int) ([]model.Link, string, error) {
	var after *time.Time
	var afterCode string
	if cursor != "" {
		t, code, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		after, afterCode = &t, code
	}

	// limit+1 rows tell us whether another page exists without a count.
	rows, err := p.pool.Query(ctx, `
		SELECT `+linkCols+` FROM links
		WHERE user_id = $1 AND deleted_at IS NULL
		  AND ($2::timestamptz IS NULL OR (created_at, short_code) < ($2, $3))
		ORDER BY created_at DESC, short_code DESC
		LIMIT $4`, userID, after, afterCode, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var links []model.Link
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, "", err
		}
		links = append(links, l)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	if len(links) <= limit {
		return links, "", nil
	}
	links = links[:limit]
	last := links[limit-1]
	return links, encodeCursor(last.CreatedAt, last.ShortCode), nil
}

// The cursor is base64url("<unix_micros>:<code>"): Postgres keeps
// microseconds, so the key round-trips exactly.
func encodeCursor(t time.Time, code string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(t.UnixMicro(), 10) + ":" + code))
}

func decodeCursor(c string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", ErrBadCursor
	}
	micros, code, ok := strings.Cut(string(raw), ":")
	if !ok || code == "" {
		return time.Time{}, "", ErrBadCursor
	}
	n, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return time.Time{}, "", ErrBadCursor
	}
	return time.UnixMicro(n), code, nil
}

// ownerEditable is the predicate for rows an owner may still change: never
// disabled, or disabled by its owner (D6). An M1 SQL takedown has a NULL
// disabled_by, which must read as "not the owner", not as NULL.
const ownerEditable = `(disabled_at IS NULL OR COALESCE(disabled_by LIKE 'user:%', false))`

func (p *Postgres) SetDisabledByOwner(ctx context.Context, userID int64, code string, disabled bool) error {
	sql := `UPDATE links SET disabled_at = NULL, disabled_by = NULL, disable_reason = NULL
	        WHERE short_code = $2 AND user_id = $1 AND deleted_at IS NULL AND disabled_by LIKE 'user:%'`
	if disabled {
		sql = `UPDATE links SET disabled_at = now(), disabled_by = 'user:' || $1::text, disable_reason = NULL
		       WHERE short_code = $2 AND user_id = $1 AND deleted_at IS NULL AND disabled_at IS NULL`
	}
	tag, err := p.pool.Exec(ctx, sql, userID, code)
	if err != nil || tag.RowsAffected() == 1 {
		return err
	}
	return p.whyUnchanged(ctx, userID, code)
}

func (p *Postgres) DeleteByOwner(ctx context.Context, userID int64, code string) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE links SET deleted_at = now()
		 WHERE short_code = $2 AND user_id = $1 AND deleted_at IS NULL AND `+ownerEditable, userID, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	if err := p.whyUnchanged(ctx, userID, code); err != nil {
		return err
	}
	return ErrNotFound // an editable row that wasn't deleted: it vanished meanwhile
}

// whyUnchanged explains a conditional write that touched no row: the owner
// has no such link, it is taken down, or it was already in the asked state.
func (p *Postgres) whyUnchanged(ctx context.Context, userID int64, code string) error {
	var editable bool
	err := p.pool.QueryRow(ctx,
		`SELECT `+ownerEditable+` FROM links WHERE short_code = $2 AND user_id = $1 AND deleted_at IS NULL`, userID, code).Scan(&editable)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return err
	case !editable:
		return ErrTakenDown
	default:
		return nil
	}
}

func (p *Postgres) Takedown(ctx context.Context, code, actor, reason string) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE links SET disabled_at = now(), disabled_by = 'operator:' || $2, disable_reason = $3
		 WHERE short_code = $1`, code, actor, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
		&l.DisabledAt, &l.DisabledBy, &l.DisableReason, &l.CreatedAt, &l.DeletedAt,
	)
	return l, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
