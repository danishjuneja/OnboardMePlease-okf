package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"onboardmeplease/internal/api"
	"onboardmeplease/internal/config"
	"onboardmeplease/internal/db"
	"onboardmeplease/internal/index"
	"onboardmeplease/internal/jobs"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "parse-go" {
		return index.RunParser(os.Stdin, os.Stdout)
	}
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	settings, err := config.Load()
	if err != nil {
		return err
	}
	switch command {
	case "doctor":
		return doctor(settings)
	case "migrate":
		pool, closePool, err := connect(settings)
		if err != nil {
			return err
		}
		defer closePool()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		fmt.Println("Migrations complete")
		return nil
	case "serve":
		return serve(settings)
	default:
		return errors.New("usage: onboardmeplease [doctor|migrate|serve]")
	}
}

func connect(settings config.Config) (*pgxpool.Pool, func(), error) {
	if settings.DatabaseURL == "" {
		return nil, nil, errors.New("database connection is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, settings.DatabaseURL)
	if err != nil {
		return nil, nil, errors.New("database connection configuration is invalid")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, errors.New("database is unavailable")
	}
	return pool, pool.Close, nil
}

func doctor(settings config.Config) error {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Println("Git: unavailable")
		return errors.New("Git is required for repository capture")
	}
	fmt.Println("Git: available")
	fmt.Println("Mode:", settings.ModelMode)
	if err := os.MkdirAll(settings.DataDir, 0700); err != nil {
		return errors.New("application data directory is not writable")
	}
	fmt.Println("Data directory: writable")
	if settings.DatabaseURL == "" {
		fmt.Println("Database: not configured")
		return nil
	}
	pool, closePool, err := connect(settings)
	if err != nil {
		fmt.Println("Database: unavailable")
		return err
	}
	defer closePool()
	var extensionAvailable bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_available_extensions WHERE name='vector')`).Scan(&extensionAvailable); err != nil {
		return errors.New("cannot inspect database extensions")
	}
	if !extensionAvailable {
		return errors.New("pgvector extension is not available")
	}
	fmt.Println("Database: available with pgvector")
	return nil
}

func serve(settings config.Config) error {
	if err := os.MkdirAll(settings.DataDir, 0700); err != nil {
		return errors.New("application data directory is not writable")
	}
	pool, closePool, err := connect(settings)
	if err != nil {
		return err
	}
	defer closePool()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	migrationContext, cancel := context.WithTimeout(ctx, 60*time.Second)
	if err := db.Migrate(migrationContext, pool); err != nil {
		cancel()
		return err
	}
	cancel()
	client, err := jobs.NewClient(pool, settings)
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return errors.New("cannot start background worker")
	}
	server := &http.Server{Addr: settings.ListenAddr, Handler: api.New(pool, client, settings).Handler(), ReadHeaderTimeout: 5 * time.Second}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	fmt.Println("OnboardMePlease listening at http://" + settings.PublicHost)
	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP service stopped unexpectedly")
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	_ = client.Stop(shutdownCtx)
	return nil
}
