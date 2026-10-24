package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/azizhudaya/shortlink/api/internal/model"
)

const userCols = `id, google_sub, email, is_active, is_whitelisted, created_at`

func scanUser(row scannable) (model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.GoogleSub, &u.Email, &u.IsActive, &u.IsWhitelisted, &u.CreatedAt)
	return u, err
}

func (p *Postgres) UpsertGoogleUser(ctx context.Context, sub, email string, allowNew bool) (model.User, bool, error) {
	// Two passes at most: if a concurrent first sign-in wins the INSERT race,
	// ON CONFLICT DO NOTHING returns no row and the second pass finds it.
	for attempt := 0; attempt < 2; attempt++ {
		u, err := scanUser(p.pool.QueryRow(ctx,
			`SELECT `+userCols+` FROM users WHERE google_sub = $1`, sub))
		switch {
		case err == nil:
			if !u.IsActive {
				return model.User{}, false, ErrUserDisabled
			}
			u, err = scanUser(p.pool.QueryRow(ctx,
				`UPDATE users SET email = $2, last_login_at = now() WHERE id = $1 RETURNING `+userCols,
				u.ID, email))
			return u, false, err
		case !errors.Is(err, pgx.ErrNoRows):
			return model.User{}, false, err
		}

		if !allowNew {
			return model.User{}, false, ErrSignupsClosed
		}
		u, err = scanUser(p.pool.QueryRow(ctx,
			`INSERT INTO users (google_sub, email, last_login_at) VALUES ($1, $2, now())
			 ON CONFLICT (google_sub) DO NOTHING RETURNING `+userCols, sub, email))
		if err == nil {
			return u, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, false, err
		}
	}
	return model.User{}, false, fmt.Errorf("upserting user %q: lost the insert race twice", sub)
}

func (p *Postgres) CreateSession(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time, userAgent, ip string) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at, user_agent, ip)
		 VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::inet)`,
		tokenHash, userID, expiresAt, userAgent, ip)
	return err
}

func (p *Postgres) GetSession(ctx context.Context, tokenHash []byte) (model.Session, error) {
	var s model.Session
	var active bool
	err := p.pool.QueryRow(ctx,
		`SELECT s.user_id, u.email, s.expires_at, u.is_active
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1`, tokenHash).Scan(&s.UserID, &s.Email, &s.ExpiresAt, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Session{}, ErrSessionInvalid
	}
	if err != nil {
		return model.Session{}, err
	}
	if !time.Now().Before(s.ExpiresAt) {
		if err := p.DeleteSession(ctx, tokenHash); err != nil {
			return model.Session{}, err
		}
		return model.Session{}, ErrSessionInvalid
	}
	if !active {
		return model.Session{}, ErrSessionInvalid
	}
	return s, nil
}

func (p *Postgres) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (p *Postgres) PurgeExpired(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `DELETE FROM link_creations WHERE created_at < now() - interval '24 hours'`)
	return err
}

func (p *Postgres) BanUser(ctx context.Context, email, actor, reason string) (model.User, int, int, error) {
	id, err := p.resolveEmail(ctx, email, "is_active")
	if err != nil {
		return model.User{}, 0, 0, err
	}
	links, sessions, err := p.BanUserByID(ctx, id, "operator:"+actor, reason)
	if err != nil {
		return model.User{}, 0, 0, err
	}
	u, err := scanUser(p.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
	return u, links, sessions, err
}

// BanUserByID is the D26 cascade with an explicit actor ("operator:<name>"
// or "system:safebrowsing"). Owner-disabled links are taken down too, so the
// owner cannot re-enable them after a lifted ban (D6).
func (p *Postgres) BanUserByID(ctx context.Context, id int64, disabledBy, reason string) (links, sessions int, err error) {
	err = pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET is_active = false WHERE id = $1`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id)
		if err != nil {
			return err
		}
		sessions = int(tag.RowsAffected())
		tag, err = tx.Exec(ctx,
			`UPDATE links SET disabled_at = now(), disabled_by = $2, disable_reason = $3
			 WHERE user_id = $1 AND deleted_at IS NULL AND (disabled_at IS NULL OR disabled_by LIKE 'user:%')`,
			id, disabledBy, reason)
		if err != nil {
			return err
		}
		links = int(tag.RowsAffected())
		return nil
	})
	return links, sessions, err
}

func (p *Postgres) ListRecentUsers(ctx context.Context, since time.Time) ([]RecentUser, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT u.id, u.google_sub, u.email, u.is_active, u.is_whitelisted, u.created_at,
		        (SELECT count(*) FROM links l WHERE l.user_id = u.id)
		 FROM users u WHERE u.created_at >= $1 ORDER BY u.created_at DESC, u.id DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecentUser
	for rows.Next() {
		var r RecentUser
		u := &r.User
		if err := rows.Scan(&u.ID, &u.GoogleSub, &u.Email, &u.IsActive, &u.IsWhitelisted, &u.CreatedAt, &r.Links); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) EnableUser(ctx context.Context, email string) (model.User, error) {
	id, err := p.resolveEmail(ctx, email, "NOT is_active")
	if err != nil {
		return model.User{}, err
	}
	return scanUser(p.pool.QueryRow(ctx,
		`UPDATE users SET is_active = true WHERE id = $1 RETURNING `+userCols, id))
}

func (p *Postgres) SetWhitelisted(ctx context.Context, email string, on bool) (model.User, error) {
	id, err := p.resolveEmail(ctx, email, "is_active")
	if err != nil {
		return model.User{}, err
	}
	return scanUser(p.pool.QueryRow(ctx,
		`UPDATE users SET is_whitelisted = $2 WHERE id = $1 RETURNING `+userCols, id, on))
}

// resolveEmail finds the one account an operator command means. Emails are
// matched case-insensitively. When several accounts share the address, those
// matching `prefer` (a fixed SQL predicate) are the candidates if any exist;
// more than one candidate is ErrAmbiguousEmail.
func (p *Postgres) resolveEmail(ctx context.Context, email, prefer string) (int64, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, `+prefer+` FROM users WHERE lower(email) = lower($1) ORDER BY id`, email)
	if err != nil {
		return 0, err
	}
	var all, preferred []int64
	for rows.Next() {
		var id int64
		var match bool
		if err := rows.Scan(&id, &match); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, id)
		if match {
			preferred = append(preferred, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	candidates := all
	if len(preferred) > 0 {
		candidates = preferred
	}
	switch len(candidates) {
	case 0:
		return 0, ErrNotFound
	case 1:
		return candidates[0], nil
	default:
		return 0, fmt.Errorf("%w: user ids %v", ErrAmbiguousEmail, candidates)
	}
}
