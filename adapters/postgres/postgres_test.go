package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool connects to the compose/CI database, migrates, and truncates, so
// every test starts from a known-empty, fully-migrated schema.
func testPool(test *testing.T) *pgxpool.Pool {
	test.Helper()
	databaseURL := os.Getenv("DNSSHIMPLE_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://dnsshimple:dnsshimple@localhost:5433/dnsshimple"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := Connect(ctx, databaseURL)
	if err != nil {
		test.Skipf("no test database reachable (docker compose up -d): %v", err)
	}
	test.Cleanup(pool.Close)

	if err := Migrate(ctx, pool); err != nil {
		test.Fatalf("migrating the test database: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`TRUNCATE tenants, ssh_keys, pending_links RESTART IDENTITY CASCADE`); err != nil {
		test.Fatalf("truncating the test database: %v", err)
	}
	return pool
}

func TestMigrateIsIdempotentAndLeavesTheIdentitySchemaInPlace(test *testing.T) {
	// Given a migrated database (testPool migrated once already)
	pool := testPool(test)
	ctx := context.Background()

	// When Migrate runs again
	if err := Migrate(ctx, pool); err != nil {
		test.Fatalf("a second Migrate must be a no-op, got: %v", err)
	}

	// Then the identity tables exist
	for _, table := range []string{"tenants", "ssh_keys", "pending_links"} {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table).Scan(&exists)
		if err != nil || !exists {
			test.Fatalf("table %q missing after migration (err: %v)", table, err)
		}
	}
}
