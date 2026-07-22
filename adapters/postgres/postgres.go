// Package postgres is the driven adapter for everything persistent: the
// connection pool, the schema migrator, and the Component repositories.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens the process-wide pool and verifies the database answers, so
// a bad DATABASE_URL fails the boot instead of the first request.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing the database configuration: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("the database did not answer: %w", err)
	}
	return pool, nil
}
