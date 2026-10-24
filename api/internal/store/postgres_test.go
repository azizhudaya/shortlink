package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/model"
)

func TestCreateStampsOwner(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")

	gen, err := st.CreateWithGeneratedCode(ctx, id, "https://example.com/a")
	if err != nil || gen.UserID == nil || *gen.UserID != id {
		t.Fatalf("generated: %+v, %v", gen, err)
	}
	al, err := st.CreateWithAlias(ctx, id, "mine", "https://example.com/b")
	if err != nil || al.UserID == nil || *al.UserID != id || !al.IsCustom {
		t.Fatalf("alias: %+v, %v", al, err)
	}
	if gen.Status() != "active" {
		t.Fatalf("status = %q", gen.Status())
	}
}

func TestCreateRecordsCreation(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	_, _ = st.CreateWithGeneratedCode(ctx, id, "https://example.com/a")
	_, _ = st.CreateWithAlias(ctx, id, "mine", "https://example.com/b")
	// A failed alias insert must not consume quota.
	if _, err := st.CreateWithAlias(ctx, id, "mine", "https://example.com/c"); !errors.Is(err, ErrAliasTaken) {
		t.Fatalf("err = %v, want ErrAliasTaken", err)
	}

	if n := count(t, pool, `SELECT count(*) FROM link_creations WHERE user_id = $1`, id); n != 2 {
		t.Fatalf("link_creations = %d, want 2", n)
	}
}

func TestQuotaSurvivesDelete(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	start := time.Now()
	var codes []string
	for i := 0; i < 3; i++ {
		l, err := st.CreateWithGeneratedCode(ctx, id, "https://example.com")
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, l.ShortCode)
	}
	for _, c := range codes {
		if err := st.DeleteByOwner(ctx, id, c); err != nil {
			t.Fatal(err)
		}
	}

	n, oldest, accountCreated, whitelisted, err := st.CreationQuota(ctx, id)
	if err != nil || n != 3 || whitelisted {
		t.Fatalf("CreationQuota = %d, whitelisted %v, err %v; want 3, false", n, whitelisted, err)
	}
	if oldest.Before(start.Add(-time.Second)) || oldest.After(time.Now()) {
		t.Fatalf("oldest = %v, want around %v", oldest, start)
	}
	if time.Since(accountCreated) > time.Minute {
		t.Fatalf("accountCreated = %v", accountCreated)
	}
}

func TestCreationQuotaEmptyAndWhitelisted(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	if _, err := pool.Exec(ctx, `INSERT INTO link_creations (user_id, created_at) VALUES ($1, now() - interval '25 hours')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetWhitelisted(ctx, "a@example.com", true); err != nil {
		t.Fatal(err)
	}

	n, oldest, _, whitelisted, err := st.CreationQuota(ctx, id)
	if err != nil || n != 0 || !oldest.IsZero() || !whitelisted {
		t.Fatalf("CreationQuota = %d, %v, whitelisted %v, err %v; want 0, zero time, true", n, oldest, whitelisted, err)
	}
}

func TestListOwnerScopedNewestFirst(t *testing.T) {
	st, _ := newTestStore(t)
	a := mustUser(t, st, "sub-a", "a@example.com")
	b := mustUser(t, st, "sub-b", "b@example.com")
	_, _ = st.CreateWithAlias(ctx, a, "first", "https://example.com/1")
	_, _ = st.CreateWithAlias(ctx, b, "theirs", "https://example.com/x")
	_, _ = st.CreateWithAlias(ctx, a, "second", "https://example.com/2")

	links, next, err := st.ListByOwner(ctx, a, "", 50)
	if err != nil || next != "" {
		t.Fatalf("err %v, next %q", err, next)
	}
	if got := codesOf(links); fmt.Sprint(got) != "[second first]" {
		t.Fatalf("codes = %v, want [second first]", got)
	}
}

func TestListPaginates(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	for i := 0; i < 120; i++ {
		if _, err := st.CreateWithGeneratedCode(ctx, id, "https://example.com"); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	var sizes []int
	cursor := ""
	for page := 0; page < 5; page++ {
		links, next, err := st.ListByOwner(ctx, id, cursor, 50)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(links))
		for _, l := range links {
			if seen[l.ShortCode] {
				t.Fatalf("code %s returned twice", l.ShortCode)
			}
			seen[l.ShortCode] = true
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if fmt.Sprint(sizes) != "[50 50 20]" || len(seen) != 120 {
		t.Fatalf("page sizes = %v, distinct = %d; want [50 50 20], 120", sizes, len(seen))
	}
}

func TestListBadCursor(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	for _, c := range []string{"not base64!", "bm9jb2xvbg", "YWJjOmRlZg"} { // "nocolon", "abc:def"
		if _, _, err := st.ListByOwner(ctx, id, c, 50); !errors.Is(err, ErrBadCursor) {
			t.Errorf("cursor %q: err = %v, want ErrBadCursor", c, err)
		}
	}
}

func TestCrossOwnerIsNotFound(t *testing.T) {
	st, _ := newTestStore(t)
	a := mustUser(t, st, "sub-a", "a@example.com")
	b := mustUser(t, st, "sub-b", "b@example.com")
	_, _ = st.CreateWithAlias(ctx, a, "mine", "https://example.com")

	if err := st.SetDisabledByOwner(ctx, b, "mine", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disable: err = %v, want ErrNotFound", err)
	}
	if err := st.DeleteByOwner(ctx, b, "mine"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete: err = %v, want ErrNotFound", err)
	}
	if l, _ := st.GetByCode(ctx, "mine"); l.Status() != "active" {
		t.Fatalf("link changed by another owner: %+v", l)
	}
}

func TestOwnerDisableEnable(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	_, _ = st.CreateWithAlias(ctx, id, "mine", "https://example.com")

	for _, step := range []struct {
		disabled bool
		want     string
	}{{true, "disabled"}, {true, "disabled"}, {false, "active"}, {false, "active"}} {
		if err := st.SetDisabledByOwner(ctx, id, "mine", step.disabled); err != nil {
			t.Fatalf("SetDisabledByOwner(%v): %v", step.disabled, err)
		}
		l, _ := st.GetByCode(ctx, "mine")
		if l.Status() != step.want {
			t.Fatalf("after SetDisabledByOwner(%v): status %q, want %q", step.disabled, l.Status(), step.want)
		}
	}
	l, _ := st.GetByCode(ctx, "mine")
	if l.DisabledBy != nil || l.DisabledAt != nil {
		t.Fatalf("re-enabled link keeps disable columns: %+v", l)
	}
}

func TestTakedownBlocksOwner(t *testing.T) {
	st, _ := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	_, _ = st.CreateWithAlias(ctx, id, "mine", "https://example.com")
	_ = st.SetDisabledByOwner(ctx, id, "mine", true)

	if err := st.Takedown(ctx, "mine", "aziz", "phishing"); err != nil {
		t.Fatal(err)
	}
	l, _ := st.GetByCode(ctx, "mine")
	if l.Status() != "taken_down" || l.DisabledBy == nil || *l.DisabledBy != "operator:aziz" ||
		l.DisableReason == nil || *l.DisableReason != "phishing" {
		t.Fatalf("after takedown: %+v", l)
	}
	for _, disabled := range []bool{false, true} {
		if err := st.SetDisabledByOwner(ctx, id, "mine", disabled); !errors.Is(err, ErrTakenDown) {
			t.Fatalf("SetDisabledByOwner(%v): err = %v, want ErrTakenDown", disabled, err)
		}
	}
	if err := st.DeleteByOwner(ctx, id, "mine"); !errors.Is(err, ErrTakenDown) {
		t.Fatalf("delete: err = %v, want ErrTakenDown", err)
	}
	if l2, _ := st.GetByCode(ctx, "mine"); *l2.DisabledBy != "operator:aziz" {
		t.Fatalf("owner changed a taken-down row: %+v", l2)
	}
}

func TestM1NullDisabledByIsTakenDown(t *testing.T) {
	st, pool := newTestStore(t)
	id := mustUser(t, st, "sub-a", "a@example.com")
	if _, err := pool.Exec(ctx,
		`INSERT INTO links (short_code, long_url, user_id, disabled_at) VALUES ('m1', 'https://example.com', $1, now())`, id); err != nil {
		t.Fatal(err)
	}

	l, _ := st.GetByCode(ctx, "m1")
	if l.Status() != "taken_down" {
		t.Fatalf("status = %q, want taken_down", l.Status())
	}
	if err := st.SetDisabledByOwner(ctx, id, "m1", false); !errors.Is(err, ErrTakenDown) {
		t.Fatalf("err = %v, want ErrTakenDown", err)
	}
}

func TestDeleteFreesCode(t *testing.T) {
	st, _ := newTestStore(t)
	a := mustUser(t, st, "sub-a", "a@example.com")
	b := mustUser(t, st, "sub-b", "b@example.com")
	_, _ = st.CreateWithAlias(ctx, a, "mine", "https://example.com")

	if err := st.DeleteByOwner(ctx, a, "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetByCode(ctx, "mine"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: err = %v, want ErrNotFound", err)
	}
	if _, err := st.CreateWithAlias(ctx, b, "mine", "https://example.com/b"); err != nil {
		t.Fatalf("alias not reusable after delete: %v", err)
	}
}

func TestTakedownUnknown(t *testing.T) {
	st, _ := newTestStore(t)
	if err := st.Takedown(ctx, "nosuch", "aziz", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func codesOf(links []model.Link) []string {
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = l.ShortCode
	}
	return out
}
