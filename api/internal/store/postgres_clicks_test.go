package store

import (
	"testing"
)

func clicksOn(t *testing.T, st *Postgres, code, day string) int64 {
	t.Helper()
	var n int64
	err := st.pool.QueryRow(ctx,
		`SELECT COALESCE(sum(clicks), 0) FROM link_clicks_daily WHERE short_code = $1 AND day = $2::date`, code, day).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAddClicksUpserts(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	_, _ = st.CreateWithAlias(ctx, id, "mine", "https://example.com")

	if err := st.AddClicks(ctx, []ClickCount{{Code: "mine", Day: "2026-10-24", Clicks: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddClicks(ctx, []ClickCount{{Code: "mine", Day: "2026-10-24", Clicks: 2}, {Code: "mine", Day: "2026-10-25", Clicks: 1}}); err != nil {
		t.Fatal(err)
	}
	if got := clicksOn(t, st, "mine", "2026-10-24"); got != 4 {
		t.Fatalf("clicks on the 24th = %d, want 4", got)
	}
	if got := clicksOn(t, st, "mine", "2026-10-25"); got != 1 {
		t.Fatalf("clicks on the 25th = %d, want 1", got)
	}
}

func TestAddClicksSkipsUnknownCodes(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	_, _ = st.CreateWithAlias(ctx, id, "mine", "https://example.com")

	// A code that no longer exists must not fail the whole batch.
	err := st.AddClicks(ctx, []ClickCount{{Code: "nosuch", Day: "2026-10-24", Clicks: 5}, {Code: "mine", Day: "2026-10-24", Clicks: 1}})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if clicksOn(t, st, "nosuch", "2026-10-24") != 0 || clicksOn(t, st, "mine", "2026-10-24") != 1 {
		t.Fatal("unknown code stored, or known code lost")
	}
	if err := st.AddClicks(ctx, nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}
