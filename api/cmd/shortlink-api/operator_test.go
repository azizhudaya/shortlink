package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/azizhudaya/shortlink/api/internal/store"
	"github.com/azizhudaya/shortlink/api/internal/testdb"
)

var ctx = context.Background()

// newOperatorEnv points the subcommands at the migrated, empty test database.
func newOperatorEnv(t *testing.T) *store.Postgres {
	t.Helper()
	pool := testdb.New(t)
	t.Setenv("DATABASE_URL", os.Getenv("TEST_DATABASE_URL"))
	return store.NewPostgres(pool)
}

func run(t *testing.T, fn func([]string, *bytes.Buffer) error, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := fn(args, &out)
	return out.String(), err
}

func takedown(args []string, out *bytes.Buffer) error { return runTakedown(args, out) }
func user(args []string, out *bytes.Buffer) error     { return runUser(args, out) }

func TestTakedownRequiresReasonAndActor(t *testing.T) {
	newOperatorEnv(t)
	for _, args := range [][]string{
		{"-code", "x", "-reason", "r"},
		{"-code", "x", "-actor", "a"},
		{"-reason", "r", "-actor", "a"},
		{"-code", "x", "-reason", "r", "-actor", "a", "extra"},
	} {
		if _, err := run(t, takedown, args...); !errors.Is(err, errUsage) {
			t.Errorf("takedown %v: err = %v, want usage error", args, err)
		}
	}
}

func TestTakedownSetsColumns(t *testing.T) {
	st := newOperatorEnv(t)
	u, _, _ := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", true)
	if _, err := st.CreateWithAlias(ctx, u.ID, "bad", "https://example.com"); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, takedown, "-code", "bad", "-reason", "phishing", "-actor", "aziz")
	if err != nil || !regexp.MustCompile(`^disabled bad by operator:aziz at \d{4}-\d\d-\d\dT`).MatchString(out) {
		t.Fatalf("output %q, err %v", out, err)
	}
	l, _ := st.GetByCode(ctx, "bad")
	if l.Status() != "taken_down" || *l.DisabledBy != "operator:aziz" || *l.DisableReason != "phishing" {
		t.Fatalf("link = %+v", l)
	}
	if _, err := run(t, takedown, "-code", "nosuch", "-reason", "r", "-actor", "a"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown code: err = %v", err)
	}
}

func TestUserDisableCascades(t *testing.T) {
	st := newOperatorEnv(t)
	u, _, _ := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", true)
	_, _ = st.CreateWithAlias(ctx, u.ID, "one", "https://example.com/1")
	_, _ = st.CreateWithAlias(ctx, u.ID, "two", "https://example.com/2")
	_ = st.CreateSession(ctx, []byte("s"), u.ID, u.CreatedAt.Add(1e15), "", "")

	out, err := run(t, user, "disable", "-email", "a@example.com", "-reason", "spam", "-actor", "aziz")
	want := "disabled user 1 (a@example.com): 2 links taken down, 1 sessions ended\n"
	if err != nil || out != want {
		t.Fatalf("output %q, err %v; want %q", out, err, want)
	}
	if err := st.SetDisabledByOwner(ctx, u.ID, "one", false); !errors.Is(err, store.ErrTakenDown) {
		t.Fatalf("owner re-enable after ban: err = %v, want ErrTakenDown", err)
	}
}

func TestUserDisableRequiresFlags(t *testing.T) {
	newOperatorEnv(t)
	for _, args := range [][]string{
		{"disable", "-email", "a@example.com", "-reason", "r"},
		{"disable", "-email", "a@example.com", "-actor", "a"},
		{"disable", "-reason", "r", "-actor", "a"},
		{},
		{"frobnicate"},
	} {
		if _, err := run(t, user, args...); !errors.Is(err, errUsage) {
			t.Errorf("user %v: err = %v, want usage error", args, err)
		}
	}
}

func TestUserEnableRequiresFlags(t *testing.T) {
	newOperatorEnv(t)
	for _, args := range [][]string{{"enable", "-email", "a@example.com"}, {"enable", "-actor", "a"}} {
		if _, err := run(t, user, args...); !errors.Is(err, errUsage) {
			t.Errorf("user %v: err = %v, want usage error", args, err)
		}
	}
}

func TestUserEnableThenSignInWorks(t *testing.T) {
	st := newOperatorEnv(t)
	u, _, _ := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", true)
	_, _ = st.CreateWithAlias(ctx, u.ID, "one", "https://example.com/1")
	if _, err := run(t, user, "disable", "-email", "a@example.com", "-reason", "x", "-actor", "aziz"); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, user, "enable", "-email", "a@example.com", "-actor", "aziz")
	want := "enabled user 1 (a@example.com); its taken-down links were not restored\n"
	if err != nil || out != want {
		t.Fatalf("output %q, err %v; want %q", out, err, want)
	}
	if _, _, err := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", false); err != nil {
		t.Fatalf("sign-in after enable: %v", err)
	}
	if l, _ := st.GetByCode(ctx, "one"); l.Status() != "taken_down" {
		t.Fatalf("link restored by enable: %+v", l)
	}
}

func TestUserWhitelistOnOff(t *testing.T) {
	st := newOperatorEnv(t)
	u, _, _ := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", true)

	out, err := run(t, user, "whitelist", "-email", "a@example.com")
	if err != nil || out != "whitelisted user 1 (a@example.com)\n" {
		t.Fatalf("on: %q, %v", out, err)
	}
	if _, _, _, wl, _ := st.CreationQuota(ctx, u.ID); !wl {
		t.Fatal("not whitelisted")
	}
	out, err = run(t, user, "whitelist", "-email", "a@example.com", "-off")
	if err != nil || out != "removed whitelist for user 1 (a@example.com)\n" {
		t.Fatalf("off: %q, %v", out, err)
	}
	if _, err := run(t, user, "whitelist"); !errors.Is(err, errUsage) {
		t.Fatalf("no email: err = %v", err)
	}
	if _, err := run(t, user, "whitelist", "-email", "nobody@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown email: err = %v", err)
	}
}

func TestUserListRecent(t *testing.T) {
	st := newOperatorEnv(t)
	a, _, _ := st.UpsertGoogleUser(ctx, "sub-a", "a@example.com", true)
	_, _, _ = st.UpsertGoogleUser(ctx, "sub-b", "b@example.com", true)
	_, _ = st.CreateWithAlias(ctx, a.ID, "one", "https://example.com/1")
	_, _ = st.SetWhitelisted(ctx, "b@example.com", true)

	out, err := run(t, user, "list-recent", "-days", "7")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 ||
		!regexp.MustCompile(`^2  \S+  b@example\.com  links=0  active=true  whitelisted=true$`).MatchString(lines[0]) ||
		!regexp.MustCompile(`^1  \S+  a@example\.com  links=1  active=true  whitelisted=false$`).MatchString(lines[1]) {
		t.Fatalf("list-recent output:\n%s", out)
	}
}
