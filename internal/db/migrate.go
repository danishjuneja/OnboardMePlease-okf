package db

import (
	"context"
	"embed"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const migrationLockID int64 = 0x4f4d505f4d494752

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return errors.New("cannot acquire migration connection")
	}
	defer connection.Release()
	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return errors.New("cannot lock migrations")
	}
	defer connection.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockID)

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return errors.New("cannot initialize River migrations")
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return errors.New("River migration failed")
	}

	transaction, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("cannot begin application migration")
	}
	defer transaction.Rollback(context.Background())
	if _, err := transaction.Exec(ctx, `CREATE TABLE IF NOT EXISTS app_migrations (
        version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return errors.New("cannot initialize application migrations")
	}
	for _, migration := range []struct {
		version int
		file    string
	}{{1, "migrations/001_foundation.sql"}, {2, "migrations/002_github_only.sql"}, {3, "migrations/003_evidence_search.sql"}, {4, "migrations/004_embeddings.sql"}} {
		var applied bool
		if err := transaction.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_migrations WHERE version=$1)`, migration.version).Scan(&applied); err != nil {
			return errors.New("cannot inspect application migrations")
		}
		if applied {
			continue
		}
		sql, err := migrationFiles.ReadFile(migration.file)
		if err != nil {
			return errors.New("application migration is missing")
		}
		if _, err := transaction.Exec(ctx, string(sql)); err != nil {
			return errors.New("application migration failed")
		}
		if _, err := transaction.Exec(ctx, `INSERT INTO app_migrations (version) VALUES ($1)`, migration.version); err != nil {
			return errors.New("cannot record application migration")
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return errors.New("cannot commit application migration")
	}
	return nil
}
