package postgres

import (
	"context"
	"testing"

	"dnsshimple/core/identity"
)

func TestStatisticsDescribeTheDatabaseWithoutTouchingComponentTables(test *testing.T) {
	statistics := NewStatistics(testPool(test))
	ctx := context.Background()

	size := statistics.DatabaseBytes(ctx)
	if size.Failed() || size.Value() <= 0 {
		test.Fatalf("the database must have a size: %d / %v", size.Value(), size.Error())
	}

	tables := statistics.Tables(ctx)
	if tables.Failed() {
		test.Fatalf("listing tables: %v", tables.Error())
	}
	names := map[string]bool{}
	for _, stat := range tables.Value() {
		names[stat.Name] = true
		if stat.TotalBytes <= 0 {
			test.Fatalf("%s must occupy space, got %d bytes", stat.Name, stat.TotalBytes)
		}
	}
	for _, expected := range []string{"tenants", "ssh_keys", "pending_links"} {
		if !names[expected] {
			test.Fatalf("expected %s among the table stats, got %v", expected, names)
		}
	}
}

func TestVacuumAnalyzeMakesThePlannerEstimatesCatchUp(test *testing.T) {
	// Given three freshly bound tenants the estimates have not seen
	pool := testPool(test)
	repository := NewIdentityRepository(pool)
	ctx := context.Background()
	for _, account := range []int64{1, 2, 3} {
		bound := repository.BindTenant(ctx, "SHA256:laptop",
			identity.RemoteIdentity{AccountID: account, Email: "guille@sendia.org"}, []byte("sealed"))
		if bound.Failed() {
			test.Fatalf("seeding tenant %d: %v", account, bound.Error())
		}
	}

	// When the database is vacuumed and re-analyzed
	if vacuumed := NewMaintenance(pool).VacuumAnalyze(ctx); vacuumed.Failed() {
		test.Fatalf("vacuuming: %v", vacuumed.Error())
	}

	// Then the tenants estimate reports the true row count
	tables := NewStatistics(pool).Tables(ctx)
	if tables.Failed() {
		test.Fatalf("listing tables: %v", tables.Error())
	}
	for _, stat := range tables.Value() {
		if stat.Name == "tenants" {
			if stat.LiveRows != 3 {
				test.Fatalf("the estimate must catch up to 3 live rows, got %d", stat.LiveRows)
			}
			return
		}
	}
	test.Fatal("tenants missing from the table stats")
}
