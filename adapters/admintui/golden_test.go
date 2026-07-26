package admintui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dnsshimple/core/admin"
	"dnsshimple/core/identity"
)

var updateGoldens = flag.Bool("update", false, "rewrite the golden screen captures")

// Golden captures pin the console layout, same stance as the sshui suite:
// the renderer writes to io.Discard, so styles degrade to plain text.
func assertGolden(test *testing.T, name, rendered string) {
	test.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			test.Fatalf("writing %s: %v", path, err)
		}
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		test.Fatalf("reading %s (run with -update to create it): %v", path, err)
	}
	if string(expected) != rendered {
		test.Fatalf("%s drifted from its golden capture; rerun with -update if intended.\n--- got ---\n%s", name, rendered)
	}
}

func TestAdminScreensMatchTheirGoldenCaptures(test *testing.T) {
	harness := newHarness(test)

	harness.deliver(test, overviewLoadedMessage{overview: admin.Overview{
		DatabaseBytes: 5 << 20,
		Tables: []admin.TableStat{
			{Name: "pending_links", LiveRows: 0, TotalBytes: 16384},
			{Name: "ssh_keys", LiveRows: 2, TotalBytes: 32768},
			{Name: "tenants", LiveRows: 2, TotalBytes: 65536},
		},
		JobCounts: map[string]int64{"available": 1, "completed": 41},
	}})
	assertGolden(test, "dashboard", harness.model.View())

	lastUsed := time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)
	harness.press(test, "2")
	harness.deliver(test, bindingsLoadedMessage{bindings: []identity.KeyBinding{
		{Fingerprint: "SHA256:aHR0cHM6Ly95b3V0dS5iZQ", LastUsedAt: lastUsed,
			Tenant: identity.Tenant{ID: 1, AccountID: 2416, Email: "guille@sendia.org"}},
		{Fingerprint: "SHA256:ZEfnZmVhZHNqa2ZoYWtqZg", LastUsedAt: lastUsed.Add(-time.Hour),
			Tenant: identity.Tenant{ID: 2, AccountID: 107677, Email: "guille@dnsimple.com"}},
	}})
	assertGolden(test, "bindings", harness.model.View())

	harness.press(test, "3")
	harness.deliver(test, jobsLoadedMessage{jobs: []admin.Job{
		{ID: 7, Kind: "registration_poll", State: "discarded", Attempt: 25,
			LastError: "gateway: DNSimple is struggling"},
		{ID: 9, Kind: "transfer_poll", State: "cancelled", Attempt: 3},
	}})
	assertGolden(test, "jobs", harness.model.View())
}
