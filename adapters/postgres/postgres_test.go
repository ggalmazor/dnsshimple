package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The suite runs inside its own Postgres schema (namespace) in the shared
// database, selected via search_path. Everything it creates and truncates
// lives under this schema; the dev server's tables stay in public. This
// suite once pointed at the public schema and truncated the developer's
// real linked tenants on every run.
const testSchema = "dnsshimple_tests"

// testPool connects with search_path pinned to the test schema, migrates,
// and truncates, so every test starts from a known-empty, fully-migrated
// schema without ever touching the dev tables.
func testPool(test *testing.T) *pgxpool.Pool {
	test.Helper()
	databaseURL := os.Getenv("DNSSHIMPLE_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://dnsshimple:dnsshimple@localhost:5433/dnsshimple"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		test.Fatalf("bad DNSSHIMPLE_TEST_DATABASE_URL: %v", err)
	}
	configuration.ConnConfig.RuntimeParams["search_path"] = testSchema
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil || pool.Ping(ctx) != nil {
		test.Skipf("no test database reachable (docker compose up -d): %v", err)
	}
	test.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+testSchema); err != nil {
		test.Fatalf("creating the test schema: %v", err)
	}
	// Fail loudly if the destructive statements below would resolve
	// anywhere else (a bad search_path would mean truncating dev tables).
	var currentSchema string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&currentSchema); err != nil || currentSchema != testSchema {
		test.Fatalf("refusing to run destructive tests: current_schema is %q, expected %q (%v)",
			currentSchema, testSchema, err)
	}

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
