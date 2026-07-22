package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ensureTestDatabase creates the _test database on first use, connecting
// through the maintenance database from the same URL settings.
func ensureTestDatabase(ctx context.Context, testDatabase string) error {
	adminURL := os.Getenv("DNSSHIMPLE_TEST_ADMIN_DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://dnsshimple:dnsshimple@localhost:5433/dnsshimple"
	}
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close(ctx) }()

	var exists bool
	if err := admin.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, testDatabase).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	// CREATE DATABASE cannot be parameterized; the _test suffix check above
	// already constrained the name.
	_, err = admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, testDatabase))
	return err
}

// testPool connects to a dedicated test database, migrates, and truncates,
// so every test starts from a known-empty, fully-migrated schema.
//
// The database name must end in _test and is created on demand: this suite
// once pointed at the dev database and truncated the developer's real
// linked tenants on every run.
func testPool(test *testing.T) *pgxpool.Pool {
	test.Helper()
	databaseURL := os.Getenv("DNSSHIMPLE_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://dnsshimple:dnsshimple@localhost:5433/dnsshimple_test"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	parsed, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		test.Fatalf("bad DNSSHIMPLE_TEST_DATABASE_URL: %v", err)
	}
	if !strings.HasSuffix(parsed.ConnConfig.Database, "_test") {
		test.Fatalf("refusing to run destructive tests against %q: the database name must end in _test",
			parsed.ConnConfig.Database)
	}
	if err := ensureTestDatabase(ctx, parsed.ConnConfig.Database); err != nil {
		test.Skipf("no test database reachable (docker compose up -d): %v", err)
	}

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
