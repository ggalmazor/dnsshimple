package dnsimple

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
)

func TestListsWalkEveryPageDNSimpleServes(test *testing.T) {
	// Given a zone with 150 records (the fake pages at per_page=100)
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := httptest.NewServer(fake)
	test.Cleanup(server.Close)
	gateway := NewZonesGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics()))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	seeds := make([]fakeSeed, 150)
	for index := range seeds {
		seeds[index] = Seed(fmt.Sprintf("host-%03d", index), "A", "1.2.3.4", 3600)
	}
	fake.AddZone("example.com", seeds...)

	// When the records are listed
	listed := gateway.Records(context.Background(), session, "example.com")

	// Then every record arrives, across exactly two API pages
	if listed.Failed() || len(listed.Value()) != 150 {
		test.Fatalf("expected 150 records, got %d (%v)", len(listed.Value()), listed.Error())
	}
	if fake.APICalls() != 2 {
		test.Fatalf("expected 2 paged API calls, got %d", fake.APICalls())
	}
}
