package dnsimple

import (
	"context"
	"net/http/httptest"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/core/zones"
)

// Grey-box over the whole gateway stack: the zones port drives the
// rate-limited Client against the stateful fake, and assertions are the
// records DNSimple ends up holding.
func TestTheZonesGatewayRoundTripsRecordsThroughDNSimple(test *testing.T) {
	// Given a linked session and a zone with one record
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := httptest.NewServer(fake)
	test.Cleanup(server.Close)
	gateway := NewZonesGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics()))
	session := identity.Session{
		Tenant:      identity.Tenant{ID: 1, AccountID: 2416, Email: "guille@sendia.org"},
		AccessToken: "fake-access-token-2416",
	}
	seededIDs := fake.AddZone("example.com", Seed("www", "A", "1.2.3.4", 3600))
	ctx := context.Background()

	// When the zone is listed
	listedZones := gateway.Zones(ctx, session)
	if listedZones.Failed() || len(listedZones.Value()) != 1 || listedZones.Value()[0].Name != "example.com" {
		test.Fatalf("zones: %+v / %v", listedZones.Value(), listedZones.Error())
	}

	// And a record is created, updated, and listed
	created := gateway.CreateRecord(ctx, session, "example.com",
		zones.RecordDraft{Name: "api", Type: "A", Content: "5.6.7.8", TTL: 600})
	if created.Failed() || created.Value().ID == 0 {
		test.Fatalf("create: %+v / %v", created.Value(), created.Error())
	}
	updated := gateway.UpdateRecord(ctx, session, "example.com", created.Value().ID,
		zones.RecordDraft{Name: "api", Type: "A", Content: "9.9.9.9", TTL: 600})
	if updated.Failed() || updated.Value().Content != "9.9.9.9" {
		test.Fatalf("update: %+v / %v", updated.Value(), updated.Error())
	}

	// Then the listing reflects both records with the update applied
	listed := gateway.Records(ctx, session, "example.com")
	if listed.Failed() || len(listed.Value()) != 2 {
		test.Fatalf("records: %+v / %v", listed.Value(), listed.Error())
	}

	// And a batch delete of the seeded record leaves exactly the created one
	batch := gateway.SubmitBatch(ctx, session, "example.com",
		zones.BatchChange{Deletes: []int64{seededIDs[0]}})
	if batch.Failed() {
		test.Fatalf("batch: %v", batch.Error())
	}
	remaining := gateway.Records(ctx, session, "example.com")
	if remaining.Failed() || len(remaining.Value()) != 1 || remaining.Value()[0].Content != "9.9.9.9" {
		test.Fatalf("after batch: %+v / %v", remaining.Value(), remaining.Error())
	}
}
