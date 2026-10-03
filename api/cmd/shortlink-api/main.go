package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx/v5" sql driver

	"github.com/azizhudaya/shortlink/api/internal/config"
	"github.com/azizhudaya/shortlink/api/internal/httpserver"
	"github.com/azizhudaya/shortlink/api/internal/migrations"
	"github.com/azizhudaya/shortlink/api/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	// Migrate before serving, in-process. Ordering is therefore
	// unconditional: there is no window in which new schema and old code, or
	// new code and old schema, can coexist. A rollback to an image that does
	// not match the live schema fails visibly at boot rather than serving
	// wrong data.
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Error("running migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations up to date")

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("connecting to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	srv := httpserver.NewServer(cfg, store.NewPostgres(pool), logger)

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("listening", "port", cfg.Port)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown so in-flight redirects survive a compose recreate.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "error", err)
	}
}

func runMigrations(databaseURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}

	db, err := sql.Open("pgx/v5", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
