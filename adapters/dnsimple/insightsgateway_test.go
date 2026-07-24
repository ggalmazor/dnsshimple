package dnsimple

import (
	"context"
	"errors"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/insights"
	"dnsshimple/core/secondarydns"
	"dnsshimple/core/telemetry"
)

func TestInsightsAndSecondaryDNSRoundTrip(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client(), telemetry.NewMetrics())
	insightsComponent := insights.NewComponent(NewInsightsGateway(client))
	secondaryComponent := secondarydns.NewComponent(NewSecondaryDNSGateway(client))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()
	fake.AddZone("aaa.com")
	fake.AddZone("bbb.com")

	// Analytics group by zone
	volumes := insightsComponent.VolumeByZone(ctx, session)
	if volumes.Failed() || len(volumes.Value()) != 2 || volumes.Value()[0].Volume == 0 {
		test.Fatalf("analytics: %+v / %v", volumes.Value(), volumes.Error())
	}

	// TLD catalog
	tlds := insightsComponent.TLDs(ctx, session)
	if tlds.Failed() || len(tlds.Value()) != 2 || tlds.Value()[0].Name != "com" {
		test.Fatalf("tlds: %+v / %v", tlds.Value(), tlds.Error())
	}

	// Secondary DNS: validation, then the primary lifecycle
	var invalid secondarydns.InvalidPrimaryError
	if outcome := secondaryComponent.CreatePrimary(ctx, session, "ns1", "not-an-ip", 53); !errors.As(outcome.Error(), &invalid) {
		test.Fatalf("bad IP must fail typed, got %v", outcome.Error())
	}
	created := secondaryComponent.CreatePrimary(ctx, session, "ns1.primary.net", "203.0.113.53", 53)
	if created.Failed() || created.Value().ID == 0 || created.Value().Linked {
		test.Fatalf("create primary: %+v / %v", created.Value(), created.Error())
	}
	if linked := secondaryComponent.SetPrimaryLink(ctx, session, created.Value().ID, true); linked.Failed() {
		test.Fatalf("link: %v", linked.Error())
	}
	listed := secondaryComponent.Primaries(ctx, session)
	if listed.Failed() || len(listed.Value()) != 1 || !listed.Value()[0].Linked {
		test.Fatalf("primary must read linked: %+v / %v", listed.Value(), listed.Error())
	}
	if zone := secondaryComponent.CreateSecondaryZone(ctx, session, "slave.example.com"); zone.Failed() {
		test.Fatalf("secondary zone: %v", zone.Error())
	}
	zonesListed := secondaryComponent.SecondaryZones(ctx, session)
	if zonesListed.Failed() || len(zonesListed.Value()) != 1 {
		test.Fatalf("secondary zones: %+v / %v", zonesListed.Value(), zonesListed.Error())
	}
	if deleted := secondaryComponent.DeletePrimary(ctx, session, created.Value().ID); deleted.Failed() {
		test.Fatalf("delete primary: %v", deleted.Error())
	}
}

func TestTLDExtendedAttributesRoundTrip(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	component := insights.NewComponent(NewInsightsGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics())))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()

	demanding := component.ExtendedAttributes(ctx, session, "uk")
	if demanding.Failed() || len(demanding.Value()) != 1 {
		test.Fatalf("uk attributes: %+v / %v", demanding.Value(), demanding.Error())
	}
	attribute := demanding.Value()[0]
	if attribute.Name != "uk_legal_type" || !attribute.Required || len(attribute.Options) != 2 {
		test.Fatalf("uk attribute shape: %+v", attribute)
	}
	plain := component.ExtendedAttributes(ctx, session, "com")
	if plain.Failed() || len(plain.Value()) != 0 {
		test.Fatalf("com must demand nothing: %+v / %v", plain.Value(), plain.Error())
	}
}
