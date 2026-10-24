package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/azizhudaya/shortlink/api/internal/testdb"
)

var ctx = context.Background()

func newTestStore(t *testing.T) (*Postgres, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	return NewPostgres(pool), pool
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func mustUser(t *testing.T, st *Postgres, sub, email string) int64 {
	t.Helper()
	u, _, err := st.UpsertGoogleUser(ctx, sub, email, true)
	if err != nil {
		t.Fatalf("UpsertGoogleUser(%s): %v", sub, err)
	}
	return u.ID
}

// insertLink writes a link row directly; Task 5 owns the creation API.
func insertLink(t *testing.T, pool *pgxpool.Pool, code string, userID int64, disabledBy *string) {
	t.Helper()
	var disabledAt *time.Time
	if disabledBy != nil {
		now := time.Now()
		disabledAt = &now
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO links (short_code, long_url, user_id, disabled_at, disabled_by) VALUES ($1, 'https://example.com', $2, $3, $4)`,
		code, userID, disabledAt, disabledBy); err != nil {
		t.Fatalf("insert link %s: %v", code, err)
	}
}

func ptr(s string) *string { return &s }

func TestUpsertCreatesThenMatchesBySub(t *testing.T) {
	st, _ := newTestStore(t)

	u1, created, err := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", true)
	if err != nil || !created || u1.ID == 0 || !u1.IsActive || u1.IsWhitelisted {
		t.Fatalf("first = %+v, created %v, err %v", u1, created, err)
	}
	u2, created, err := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", true)
	if err != nil || created || u2.ID != u1.ID {
		t.Fatalf("second = %+v, created %v, err %v; want same user %d", u2, created, err, u1.ID)
	}
}

func TestUpsertUpdatesEmail(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "old@example.com")

	u, created, err := st.UpsertGoogleUser(ctx, "sub-a", "new@example.com", true)
	if err != nil || created || u.ID != id || u.Email != "new@example.com" {
		t.Fatalf("got %+v, created %v, err %v", u, created, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM users WHERE last_login_at IS NOT NULL`); n != 1 {
		t.Fatalf("last_login_at not set")
	}
}

func TestUpsertSignupsClosed(t *testing.T) {
	st, pool := newTestStore(t)
	mustUser(t, st, "known", "k@example.com")

	if _, _, err := st.UpsertGoogleUser(ctx, "unknown", "u@example.com", false); !errors.Is(err, ErrSignupsClosed) {
		t.Fatalf("err = %v, want ErrSignupsClosed", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM users WHERE google_sub = 'unknown'`); n != 0 {
		t.Fatalf("closed sign-up created a user")
	}
	if _, _, err := st.UpsertGoogleUser(ctx, "known", "k@example.com", false); err != nil {
		t.Fatalf("existing user refused while sign-ups closed: %v", err)
	}
}

func TestUpsertDisabledUserNotRecreated(t *testing.T) {
	st, pool := newTestStore(t)
	mustUser(t, st, "sub-a", "a@example.com")
	if _, _, _, err := st.BanUser(ctx, "a@example.com", "aziz", "spam"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", true); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("err = %v, want ErrUserDisabled", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	hash := []byte("0123456789abcdef0123456789abcdef")
	exp := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Microsecond)

	if err := st.CreateSession(ctx, hash, id, exp, "test-agent", "203.0.113.7"); err != nil {
		t.Fatal(err)
	}
	s, err := st.GetSession(ctx, hash)
	if err != nil || s.UserID != id || s.Email != "a@example.com" || !s.ExpiresAt.Equal(exp) {
		t.Fatalf("session = %+v, err %v", s, err)
	}
	if err := st.DeleteSession(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(ctx, hash); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("after delete: err = %v, want ErrSessionInvalid", err)
	}
	// An empty IP is stored as NULL rather than failing the INET cast.
	if err := st.CreateSession(ctx, []byte("other"), id, exp, "", ""); err != nil {
		t.Fatalf("session without ip/user-agent: %v", err)
	}
}

func TestExpiredSessionRejectedAndPurged(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	hash := []byte("expired")
	if err := st.CreateSession(ctx, hash, id, time.Now().Add(-time.Minute), "", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := st.GetSession(ctx, hash); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("err = %v, want ErrSessionInvalid", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM sessions`); n != 0 {
		t.Fatalf("expired session row not deleted on use")
	}
}

func TestInactiveUserSessionRejected(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	hash := []byte("live")
	if err := st.CreateSession(ctx, hash, id, time.Now().Add(time.Hour), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_active = false WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	if _, err := st.GetSession(ctx, hash); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("err = %v, want ErrSessionInvalid", err)
	}
}

func TestPurgeExpired(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	_ = st.CreateSession(ctx, []byte("old"), id, time.Now().Add(-time.Hour), "", "")
	_ = st.CreateSession(ctx, []byte("new"), id, time.Now().Add(time.Hour), "", "")
	if _, err := pool.Exec(ctx, `INSERT INTO link_creations (user_id, created_at) VALUES ($1, now() - interval '25 hours'), ($1, now())`, id); err != nil {
		t.Fatal(err)
	}

	if err := st.PurgeExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM sessions`); n != 1 {
		t.Fatalf("sessions = %d, want 1", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM link_creations`); n != 1 {
		t.Fatalf("link_creations = %d, want 1", n)
	}
}

func TestBanUserCascades(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "A@Example.com")
	other := mustUser(t, st, "sub-b", "b@example.com")
	insertLink(t, pool, "l1", id, nil)
	insertLink(t, pool, "l2", id, nil)
	insertLink(t, pool, "l3", id, ptr("user:1"))
	insertLink(t, pool, "keep", other, nil)
	_ = st.CreateSession(ctx, []byte("s1"), id, time.Now().Add(time.Hour), "", "")
	_ = st.CreateSession(ctx, []byte("s2"), id, time.Now().Add(time.Hour), "", "")
	_ = st.CreateSession(ctx, []byte("s3"), other, time.Now().Add(time.Hour), "", "")

	u, links, sessions, err := st.BanUser(ctx, "a@example.com", "aziz", "phishing")
	if err != nil || links != 3 || sessions != 2 || u.ID != id || u.IsActive {
		t.Fatalf("BanUser = %+v, %d links, %d sessions, err %v; want user %d inactive, 3, 2", u, links, sessions, err, id)
	}
	if n := count(t, pool, `SELECT count(*) FROM links WHERE user_id = $1 AND disabled_at IS NOT NULL
		AND disabled_by = 'operator:aziz' AND disable_reason = 'phishing'`, id); n != 3 {
		t.Fatalf("taken-down links = %d, want 3", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM users WHERE id = $1 AND NOT is_active`, id); n != 1 {
		t.Fatalf("user still active")
	}
	if n := count(t, pool, `SELECT count(*) FROM links WHERE short_code = 'keep' AND disabled_at IS NULL`); n != 1 {
		t.Fatalf("another user's link was taken down")
	}
	if n := count(t, pool, `SELECT count(*) FROM sessions WHERE user_id = $1`, other); n != 1 {
		t.Fatalf("another user's session was deleted")
	}
}

func TestBanUnknownEmail(t *testing.T) {
	st, _ := newTestStore(t)
	if _, _, _, err := st.BanUser(ctx, "nobody@example.com", "aziz", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestBanAmbiguousEmailChangesNothing(t *testing.T) {
	st, pool := newTestStore(t)
	mustUser(t, st, "sub-a", "same@example.com")
	mustUser(t, st, "sub-b", "same@example.com")

	if _, _, _, err := st.BanUser(ctx, "same@example.com", "aziz", "x"); !errors.Is(err, ErrAmbiguousEmail) {
		t.Fatalf("err = %v, want ErrAmbiguousEmail", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM users WHERE is_active`); n != 2 {
		t.Fatalf("ambiguous ban changed a user")
	}
}

func TestListRecentUsers(t *testing.T) {
	st, pool := newTestStore(t)
	oldID := mustUser(t, st, "sub-old", "old@example.com")
	newID := mustUser(t, st, "sub-new", "new@example.com")
	if _, err := pool.Exec(ctx, `UPDATE users SET created_at = now() - interval '10 days' WHERE id = $1`, oldID); err != nil {
		t.Fatal(err)
	}
	insertLink(t, pool, "n1", newID, nil)
	insertLink(t, pool, "n2", newID, nil)

	got, err := st.ListRecentUsers(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].User.ID != newID || got[0].Links != 2 {
		t.Fatalf("got %+v, want only the new user with 2 links", got)
	}
}

func TestEnableUserKeepsLinksDown(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	insertLink(t, pool, "l1", id, nil)
	insertLink(t, pool, "l2", id, nil)
	insertLink(t, pool, "l3", id, nil)
	if _, _, _, err := st.BanUser(ctx, "a@example.com", "aziz", "spam"); err != nil {
		t.Fatal(err)
	}

	u, err := st.EnableUser(ctx, "a@example.com")
	if err != nil || u.ID != id || !u.IsActive {
		t.Fatalf("EnableUser = %+v, %v", u, err)
	}
	if _, _, err := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", false); err != nil {
		t.Fatalf("sign-in after enable: %v", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM links WHERE user_id = $1 AND disabled_by = 'operator:aziz'`, id); n != 3 {
		t.Fatalf("taken-down links = %d, want 3 still down", n)
	}
	if _, err := st.EnableUser(ctx, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown email: err = %v, want ErrNotFound", err)
	}
}

func TestSetWhitelisted(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")

	u, err := st.SetWhitelisted(ctx, "A@example.com", true)
	if err != nil || u.ID != id || !u.IsWhitelisted {
		t.Fatalf("on: %+v, %v", u, err)
	}
	u, err = st.SetWhitelisted(ctx, "a@example.com", false)
	if err != nil || u.IsWhitelisted {
		t.Fatalf("off: %+v, %v", u, err)
	}
	if _, err := st.SetWhitelisted(ctx, "nobody@example.com", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown email: err = %v, want ErrNotFound", err)
	}
}
