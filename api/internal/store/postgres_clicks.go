package store

import "context"

// ClickCount is redirects served for one link on one UTC day (D32). Day is
// "YYYY-MM-DD" so no time zone conversion can move a count to another day.
type ClickCount struct {
	Code   string
	Day    string
	Clicks int64
}

// AddClicks adds a batch of counts in one statement. Codes that no longer
// exist are skipped rather than failing the batch. Each (code, day) must
// appear at most once.
func (p *Postgres) AddClicks(ctx context.Context, counts []ClickCount) error {
	if len(counts) == 0 {
		return nil
	}
	codes := make([]string, len(counts))
	days := make([]string, len(counts))
	clicks := make([]int64, len(counts))
	for i, c := range counts {
		codes[i], days[i], clicks[i] = c.Code, c.Day, c.Clicks
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO link_clicks_daily (short_code, day, clicks)
		SELECT c.code, c.day::date, c.clicks
		FROM unnest($1::text[], $2::text[], $3::bigint[]) AS c(code, day, clicks)
		JOIN links l ON l.short_code = c.code
		ON CONFLICT (short_code, day) DO UPDATE SET clicks = link_clicks_daily.clicks + EXCLUDED.clicks`,
		codes, days, clicks)
	return err
}
