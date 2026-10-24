package store

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRescanCandidatesWindowAndPaging(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	for _, c := range []string{"c1", "c2", "c3"} {
		_, _ = st.CreateWithAlias(ctx, id, c, "https://example.com/"+c)
	}
	_, _ = st.CreateWithAlias(ctx, id, "off", "https://example.com/off")
	_ = st.SetDisabledByOwner(ctx, id, "off", true)
	_, _ = st.CreateWithAlias(ctx, id, "old", "https://example.com/old")
	if _, err := pool.Exec(ctx, `UPDATE links SET created_at = now() - interval '31 days' WHERE short_code = 'old'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO links (short_code, long_url) VALUES ('m1', 'https://example.com/m1')`); err != nil {
		t.Fatal(err)
	}

	since := time.Now().Add(-30 * 24 * time.Hour)
	page1, err := st.RescanCandidates(ctx, since, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	page2, _ := st.RescanCandidates(ctx, since, page1[len(page1)-1].Code, 2)
	page3, _ := st.RescanCandidates(ctx, since, page2[len(page2)-1].Code, 2)
	var got []string
	for _, p := range [][]RescanLink{page1, page2, page3} {
		for _, l := range p {
			got = append(got, l.Code)
		}
	}
	if fmt.Sprint(got) != "[c1 c2 c3 m1]" || len(page3) != 0 {
		t.Fatalf("candidates = %v (page 3: %d)", got, len(page3))
	}
	c1 := page1[0]
	if c1.UserID == nil || *c1.UserID != id || !c1.UserActive || c1.UserWhitelisted || c1.LongURL != "https://example.com/c1" {
		t.Fatalf("c1 = %+v", c1)
	}
	if page2[1].UserID != nil {
		t.Fatalf("M1 link has an owner: %+v", page2[1])
	}
}

func TestSystemTakedownCountsAndIsIdempotent(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	_, _ = st.CreateWithAlias(ctx, id, "one", "https://example.com/1")
	_, _ = st.CreateWithAlias(ctx, id, "two", "https://example.com/2")

	changed, n, err := st.SystemTakedown(ctx, "one", "safe browsing: MALWARE")
	if err != nil || !changed || n != 1 {
		t.Fatalf("first: changed %v, count %d, err %v", changed, n, err)
	}
	changed, n, err = st.SystemTakedown(ctx, "one", "safe browsing: MALWARE")
	if err != nil || changed || n != 1 {
		t.Fatalf("repeat: changed %v, count %d, err %v", changed, n, err)
	}
	_, n, _ = st.SystemTakedown(ctx, "two", "safe browsing: SOCIAL_ENGINEERING")
	if n != 2 {
		t.Fatalf("count after second link = %d, want 2", n)
	}
	l, _ := st.GetByCode(ctx, "one")
	if l.Status() != "taken_down" || *l.DisabledBy != "system:safebrowsing" || *l.DisableReason != "safe browsing: MALWARE" {
		t.Fatalf("link = %+v", l)
	}
}

func TestSystemTakedownOwnerCannotUndo(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	_, _ = st.CreateWithAlias(ctx, id, "one", "https://example.com/1")
	_, _, _ = st.SystemTakedown(ctx, "one", "safe browsing: MALWARE")

	if err := st.SetDisabledByOwner(ctx, id, "one", false); !errors.Is(err, ErrTakenDown) {
		t.Fatalf("err = %v, want ErrTakenDown", err)
	}
}

func TestBanUserByIDCascades(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	_, _ = st.CreateWithAlias(ctx, id, "one", "https://example.com/1")
	_, _ = st.CreateWithAlias(ctx, id, "two", "https://example.com/2")
	_, _, _ = st.SystemTakedown(ctx, "one", "safe browsing: MALWARE")
	_ = st.CreateSession(ctx, []byte("s"), id, time.Now().Add(time.Hour), "", "")

	links, sessions, err := st.BanUserByID(ctx, id, "system:safebrowsing", "banned after safe browsing match on one")
	if err != nil || links != 1 || sessions != 1 {
		t.Fatalf("BanUserByID = %d, %d, %v; want 1 link (two), 1 session", links, sessions, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM links WHERE disabled_by = 'system:safebrowsing'`); n != 2 {
		t.Fatalf("system takedowns = %d", n)
	}
	// The cascade does not count as another flagged link.
	if _, n, _ := st.SystemTakedown(ctx, "one", "safe browsing: MALWARE"); n != 1 {
		t.Fatalf("flagged count after cascade = %d, want 1", n)
	}
}
