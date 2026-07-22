package postgres

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies every pending migration in filename order, each inside its
// own transaction, and records it in schema_migrations. Running it twice is a
// no-op, so every process can migrate on boot without coordination.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("preparing schema_migrations: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})

	for _, entry := range entries {
		applied, err := isApplied(ctx, pool, entry.Name())
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		statements, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}
		transaction, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("opening the transaction for %s: %w", entry.Name(), err)
		}
		if _, err := transaction.Exec(ctx, string(statements)); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("applying %s: %w", entry.Name(), err)
		}
		if _, err := transaction.Exec(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1)`, entry.Name()); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("recording %s: %w", entry.Name(), err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("committing %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func isApplied(ctx context.Context, pool *pgxpool.Pool, filename string) (bool, error) {
	var applied bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = $1)`, filename).Scan(&applied)
	if err != nil {
		return false, fmt.Errorf("checking whether %s is applied: %w", filename, err)
	}
	return applied, nil
}
