package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/azizhudaya/shortlink/api/internal/store"
)

// Operator subcommands run inside the container over SSH (`docker exec
// shortlink-api shortlink-api …`). They need only DATABASE_URL, work without
// Google, and never migrate: a binary older or newer than the schema refuses.

// errUsage marks a malformed command line; main exits 2 and prints usage.
var errUsage = errors.New("usage")

const usageText = `usage:
  shortlink-api [serve]
  shortlink-api takedown -code CODE -reason TEXT -actor NAME
  shortlink-api user disable -email EMAIL -reason TEXT -actor NAME
  shortlink-api user enable -email EMAIL -actor NAME
  shortlink-api user whitelist -email EMAIL [-off]
  shortlink-api user list-recent [-days N]
`

// minSchemaVersion is the first schema these subcommands understand.
const minSchemaVersion = 2

func openStore(ctx context.Context) (*store.Postgres, func(), error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, nil, errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	var version int64
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("reading schema version: %w", err)
	}
	if dirty || version < minSchemaVersion {
		pool.Close()
		return nil, nil, fmt.Errorf("schema version %d (dirty=%v) is older than %d: deploy the M2 server first",
			version, dirty, minSchemaVersion)
	}
	return store.NewPostgres(pool), pool.Close, nil
}

// parse parses flags and requires the named ones to be non-empty.
func parse(fs *flag.FlagSet, args []string, required ...string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %s: %v", errUsage, fs.Name(), err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: %s: unexpected argument %q", errUsage, fs.Name(), fs.Arg(0))
	}
	for _, name := range required {
		if fs.Lookup(name).Value.String() == "" {
			return fmt.Errorf("%w: %s: -%s is required", errUsage, fs.Name(), name)
		}
	}
	return nil
}

// runTakedown disables one link as operator:<actor> (D4).
func runTakedown(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("takedown", flag.ContinueOnError)
	code := fs.String("code", "", "short code to take down")
	reason := fs.String("reason", "", "why, for the audit trail")
	actor := fs.String("actor", "", "who is taking it down")
	if err := parse(fs, args, "code", "reason", "actor"); err != nil {
		return err
	}

	ctx := context.Background()
	st, closeStore, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer closeStore()
	if err := st.Takedown(ctx, *code, *actor, *reason); err != nil {
		return fmt.Errorf("taking down %s: %w", *code, err)
	}
	fmt.Fprintf(out, "disabled %s by operator:%s at %s\n", *code, *actor, time.Now().UTC().Format(time.RFC3339))
	return nil
}

// runUser dispatches the account subcommands (D26, D28, D29).
func runUser(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: user needs a subcommand", errUsage)
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("user "+sub, flag.ContinueOnError)
	email := fs.String("email", "", "account email (case-insensitive)")

	var run func(ctx context.Context, st *store.Postgres) error
	switch sub {
	case "disable":
		reason := fs.String("reason", "", "why, recorded on every link taken down")
		actor := fs.String("actor", "", "who is banning")
		if err := parse(fs, args, "email", "reason", "actor"); err != nil {
			return err
		}
		run = func(ctx context.Context, st *store.Postgres) error {
			u, links, sessions, err := st.BanUser(ctx, *email, *actor, *reason)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "disabled user %d (%s): %d links taken down, %d sessions ended\n", u.ID, u.Email, links, sessions)
			return nil
		}
	case "enable":
		actor := fs.String("actor", "", "who is lifting the ban")
		if err := parse(fs, args, "email", "actor"); err != nil {
			return err
		}
		run = func(ctx context.Context, st *store.Postgres) error {
			u, err := st.EnableUser(ctx, *email)
			if err != nil {
				return err
			}
			slog.Info("user enabled", "user_id", u.ID, "actor", *actor)
			fmt.Fprintf(out, "enabled user %d (%s); its taken-down links were not restored\n", u.ID, u.Email)
			return nil
		}
	case "whitelist":
		off := fs.Bool("off", false, "remove the whitelist instead")
		if err := parse(fs, args, "email"); err != nil {
			return err
		}
		run = func(ctx context.Context, st *store.Postgres) error {
			u, err := st.SetWhitelisted(ctx, *email, !*off)
			if err != nil {
				return err
			}
			if *off {
				fmt.Fprintf(out, "removed whitelist for user %d (%s)\n", u.ID, u.Email)
			} else {
				fmt.Fprintf(out, "whitelisted user %d (%s)\n", u.ID, u.Email)
			}
			return nil
		}
	case "list-recent":
		days := fs.Int("days", 7, "how far back to look")
		if err := parse(fs, args); err != nil {
			return err
		}
		run = func(ctx context.Context, st *store.Postgres) error {
			users, err := st.ListRecentUsers(ctx, time.Now().Add(-time.Duration(*days)*24*time.Hour))
			if err != nil {
				return err
			}
			for _, r := range users {
				fmt.Fprintf(out, "%d  %s  %s  links=%d  active=%v  whitelisted=%v\n",
					r.User.ID, r.User.CreatedAt.UTC().Format(time.RFC3339), r.User.Email, r.Links, r.User.IsActive, r.User.IsWhitelisted)
			}
			return nil
		}
	default:
		return fmt.Errorf("%w: unknown user subcommand %q", errUsage, sub)
	}

	ctx := context.Background()
	st, closeStore, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer closeStore()
	return run(ctx, st)
}
