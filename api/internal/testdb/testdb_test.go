package testdb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/azizhudaya/shortlink/api/internal/migrations"
)

// The M1→M2 upgrade runs against the live database exactly once, so it is
// tested from a real M1-shaped schema rather than from an empty one.
func TestMigrationsApplyOverM1Data(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()

	if err := newMigrate(t, url).Drop(); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := newMigrate(t, url).Migrate(1); err != nil {
		t.Fatalf("migrate to 1: %v", err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// An M1 row: no owner, taken down by hand with SQL.
	if _, err := pool.Exec(ctx,
		`INSERT INTO links (short_code, long_url, disabled_at) VALUES ('m1code', 'https://example.com', now())`); err != nil {
		t.Fatalf("insert M1 row: %v", err)
	}

	if err := migrations.Up(url); err != nil {
		t.Fatalf("Up: %v", err)
	}

	var disabledBy *string
	var userID *int64
	var deletedAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT disabled_by, user_id, deleted_at FROM links WHERE short_code = 'm1code'`).Scan(&disabledBy, &userID, &deletedAt); err != nil {
		t.Fatalf("M1 row did not survive: %v", err)
	}
	if disabledBy != nil || userID != nil || deletedAt != nil {
		t.Fatalf("disabled_by = %v, user_id = %v, deleted_at = %v; want all NULL", disabledBy, userID, deletedAt)
	}

	_, err = pool.Exec(ctx, `UPDATE links SET user_id = 999 WHERE short_code = 'm1code'`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("user_id = 999: err = %v, want foreign-key violation 23503", err)
	}

	if err := migrations.Up(url); err != nil {
		t.Fatalf("second Up: %v", err)
	}
}

func newMigrate(t *testing.T, url string) *migrate.Migrate {
	t.Helper()
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx/v5", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
