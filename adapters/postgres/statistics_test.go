package postgres

import (
	"context"
	"testing"
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
