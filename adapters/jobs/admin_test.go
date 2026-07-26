package jobs

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// The jobs admin suite is grey-box against real River tables in the shared
// dnsshimple_tests schema (same isolation stance as adapters/postgres):
// jobs are inserted through an insert-only client, never by direct SQL.
const testSchema = "dnsshimple_tests"

func testHarness(test *testing.T) (Admin, *river.Client[pgx.Tx]) {
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
	var currentSchema string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&currentSchema); err != nil || currentSchema != testSchema {
		test.Fatalf("refusing to run destructive tests: current_schema is %q, expected %q (%v)",
			currentSchema, testSchema, err)
	}

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		test.Fatalf("building the River migrator: %v", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		test.Fatalf("migrating the River schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE river_job`); err != nil {
		test.Fatalf("truncating river_job: %v", err)
	}

	// An insert-only client: no queues, no workers - exactly what the admin
	// process runs with, so nothing here can start working jobs.
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		test.Fatalf("building the insert-only client: %v", err)
	}
	return NewAdmin(client, pool), client
}

func TestJobAdministrationCountsListsRetriesAndDeletes(test *testing.T) {
	administration, client := testHarness(test)
	ctx := context.Background()

	// Given one waiting poll and one that an operator cancelled
	waiting, err := client.Insert(ctx, RegistrationPollArgs{TenantID: 1, DomainName: "alpha.com", RegistrationID: 11}, nil)
	if err != nil {
		test.Fatalf("inserting the waiting job: %v", err)
	}
	troubled, err := client.Insert(ctx, TransferPollArgs{TenantID: 1, DomainName: "beta.org", TransferID: 22}, nil)
	if err != nil {
		test.Fatalf("inserting the second job: %v", err)
	}
	if _, err := client.JobCancel(ctx, troubled.Job.ID); err != nil {
		test.Fatalf("cancelling: %v", err)
	}

	// Then the counts see both states
	counts := administration.CountsByState(ctx)
	if counts.Failed() {
		test.Fatalf("counting: %v", counts.Error())
	}
	if counts.Value()["available"] != 1 || counts.Value()["cancelled"] != 1 {
		test.Fatalf("expected one available and one cancelled, got %v", counts.Value())
	}

	// And the troubled list carries exactly the cancelled transfer poll
	listed := administration.TroubledJobs(ctx, 10)
	if listed.Failed() || len(listed.Value()) != 1 {
		test.Fatalf("expected 1 troubled job, got %v / %v", listed.Value(), listed.Error())
	}
	if listed.Value()[0].ID != troubled.Job.ID || listed.Value()[0].Kind != "transfer_poll" {
		test.Fatalf("wrong troubled job: %+v", listed.Value()[0])
	}

	// And retrying it puts it back in line
	if retried := administration.Retry(ctx, troubled.Job.ID); retried.Failed() {
		test.Fatalf("retrying: %v", retried.Error())
	}
	counts = administration.CountsByState(ctx)
	if counts.Value()["available"] != 2 {
		test.Fatalf("the retried job must be available again, got %v", counts.Value())
	}

	// And deleting the waiting one removes it outright
	if deleted := administration.Delete(ctx, waiting.Job.ID); deleted.Failed() {
		test.Fatalf("deleting: %v", deleted.Error())
	}
	counts = administration.CountsByState(ctx)
	if counts.Value()["available"] != 1 {
		test.Fatalf("exactly one job must remain, got %v", counts.Value())
	}
}
