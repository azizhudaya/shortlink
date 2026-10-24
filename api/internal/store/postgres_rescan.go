package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// RescanLink is one active link as the D27 re-scan sees it, with its owner's
// standing. Owner fields are zero for M1 links, which have no owner.
type RescanLink struct {
	Code            string
	LongURL         string
	UserID          *int64
	UserCreatedAt   time.Time
	UserActive      bool
	UserWhitelisted bool
}

// SystemSafeBrowsing is the disabled_by value of every re-scan takedown.
const SystemSafeBrowsing = "system:safebrowsing"

// FlaggedReasonPrefix starts the disable_reason of a link Safe Browsing
// itself matched, as opposed to one taken down by its owner's ban. Only these
// count toward the repeat-offender threshold, and the runbook never restores
// them.
const FlaggedReasonPrefix = "safe browsing:"

// RescanCandidates pages active links created since `since`, ordered by code.
func (p *Postgres) RescanCandidates(ctx context.Context, since time.Time, afterCode string, limit int) ([]RescanLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT l.short_code, l.long_url, l.user_id,
		       COALESCE(u.created_at, 'epoch'), COALESCE(u.is_active, false), COALESCE(u.is_whitelisted, false)
		FROM links l LEFT JOIN users u ON u.id = l.user_id
		WHERE l.disabled_at IS NULL AND l.deleted_at IS NULL AND l.created_at >= $1 AND l.short_code > $2
		ORDER BY l.short_code
		LIMIT $3`, since, afterCode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RescanLink
	for rows.Next() {
		var r RescanLink
		if err := rows.Scan(&r.Code, &r.LongURL, &r.UserID, &r.UserCreatedAt, &r.UserActive, &r.UserWhitelisted); err != nil {
			return nil, err
		}
		if r.UserID == nil {
			r.UserCreatedAt = time.Time{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SystemTakedown takes a flagged link down as system:safebrowsing. It is a
// no-op on a link already disabled. ownerFlagged is how many of the owner's
// links Safe Browsing has matched, this one included (0 without an owner).
func (p *Postgres) SystemTakedown(ctx context.Context, code, reason string) (changed bool, ownerFlagged int, err error) {
	err = pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE links SET disabled_at = now(), disabled_by = $2, disable_reason = $3
			 WHERE short_code = $1 AND disabled_at IS NULL`, code, SystemSafeBrowsing, reason)
		if err != nil {
			return err
		}
		changed = tag.RowsAffected() == 1

		var owner *int64
		if err := tx.QueryRow(ctx, `SELECT user_id FROM links WHERE short_code = $1`, code).Scan(&owner); err != nil {
			if err == pgx.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		if owner == nil {
			return nil
		}
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM links WHERE user_id = $1 AND disabled_by = $2 AND disable_reason LIKE $3`,
			*owner, SystemSafeBrowsing, FlaggedReasonPrefix+"%").Scan(&ownerFlagged)
	})
	return changed, ownerFlagged, err
}
