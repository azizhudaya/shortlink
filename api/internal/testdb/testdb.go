// Package testdb gives integration tests a migrated, empty database.
package testdb

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/azizhudaya/shortlink/api/internal/migrations"
)

// New connects to TEST_DATABASE_URL, migrates it, and truncates every table.
// It skips the test when the variable is unset, so `go test ./...` stays
// runnable without Postgres. Tests that use it must run with -p 1: they
// share one database.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	if err := migrations.Up(url); err != nil {
		t.Fatalf("migrating test database: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(context.Background(),
		`TRUNCATE links, link_creations, sessions, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncating test database: %v", err)
	}
	return pool
}
