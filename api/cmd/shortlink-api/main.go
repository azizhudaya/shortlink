package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/azizhudaya/shortlink/api/internal/auth"
	"github.com/azizhudaya/shortlink/api/internal/clicks"
	"github.com/azizhudaya/shortlink/api/internal/config"
	"github.com/azizhudaya/shortlink/api/internal/httpserver"
	"github.com/azizhudaya/shortlink/api/internal/migrations"
	"github.com/azizhudaya/shortlink/api/internal/safety"
	"github.com/azizhudaya/shortlink/api/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		serve(logger)
		return
	case "takedown":
		err = runTakedown(os.Args[2:], os.Stdout)
	case "user":
		err = runUser(os.Args[2:], os.Stdout)
	case "links":
		err = runLinks(os.Args[2:], os.Stdout)
	default:
		err = fmt.Errorf("%w: unknown command %q", errUsage, cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if errors.Is(err, errUsage) {
			fmt.Fprint(os.Stderr, usageText)
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func serve(logger *slog.Logger) {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	if missing := cfg.MissingCredentials(); len(missing) > 0 {
		logger.Error("required credentials are empty", "missing", strings.Join(missing, ","))
		os.Exit(1)
	}

	// Migrate before serving, in-process. Ordering is therefore
	// unconditional: there is no window in which new schema and old code, or
	// new code and old schema, can coexist. A rollback to an image that does
	// not match the live schema fails visibly at boot rather than serving
	// wrong data.
	if err := migrations.Up(cfg.DatabaseURL); err != nil {
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

	// No network at startup: a Google outage must not keep redirects down.
	google := auth.NewGoogleStatic(context.Background(),
		cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL)
	checker := safety.NewSafeBrowsing(safety.Endpoint, cfg.SafeBrowsingAPIKey, &http.Client{Timeout: 3 * time.Second})

	st := store.NewPostgres(pool)
	counter := clicks.New(st, logger)
	srv := httpserver.NewServer(cfg, st, st, google, checker, counter, logger)

	// Background jobs share one context, cancelled on shutdown.
	jobs, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	go purgeLoop(jobs, st, logger)
	// D32: click counts reach the database once a minute; the final flush
	// runs after the HTTP server has drained (see below).
	countsDone := make(chan struct{})
	go func() {
		counter.Run(jobs, time.Minute)
		close(countsDone)
	}()
	// D27: first pass 5 min after start, then hourly ± 5 min.
	go newRescanner(st, cfg.SafeBrowsingAPIKey, cfg.AlertWebhookURL, logger).
		Loop(jobs, 5*time.Minute, time.Hour, 5*time.Minute)

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
	// Stop the jobs only after in-flight redirects finished, so their clicks
	// are in the final flush.
	stopJobs()
	<-countsDone
}

// purgeLoop deletes expired sessions and aged-out quota rows hourly (D14).
func purgeLoop(ctx context.Context, st *store.Postgres, logger *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := st.PurgeExpired(ctx); err != nil && ctx.Err() == nil {
				logger.Error("purging expired rows", "error", err)
			}
		}
	}
}
