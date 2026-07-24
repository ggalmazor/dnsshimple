package dnsimple

import (
	"context"
	"errors"
	"testing"

	"dnsshimple/core/domains"
	"dnsshimple/core/identity"
	"dnsshimple/core/registrar"
	"dnsshimple/core/telemetry"
)

func TestDomainSettingsRoundTripThroughDNSimple(test *testing.T) {
	// Given a registered domain
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client(), telemetry.NewMetrics())
	domainsGateway := NewDomainsGateway(client)
	registrarGateway := NewRegistrarGateway(client)
	fake.AddDomain("ggalmazor.com", "registered")
	session := identity.Session{
		Tenant:      identity.Tenant{ID: 1, AccountID: 2416},
		AccessToken: "fake-access-token-2416",
	}
	ctx := context.Background()

	// A feature never touched reads as disabled, not as an error
	if privacy := registrarGateway.WhoisPrivacy(ctx, session, "ggalmazor.com"); privacy.Failed() || privacy.Value() {
		test.Fatalf("untouched whois privacy must read disabled: %v", privacy.Error())
	}

	// When DNSSEC is enabled and read back
	if outcome := domainsGateway.SetDNSSEC(ctx, session, "ggalmazor.com", true); outcome.Failed() {
		test.Fatalf("enabling DNSSEC: %v", outcome.Error())
	}
	status := domainsGateway.DNSSEC(ctx, session, "ggalmazor.com")
	if status.Failed() || !status.Value().Enabled {
		test.Fatalf("DNSSEC must read enabled: %+v / %v", status.Value(), status.Error())
	}

	// And an email forward is created, listed, and deleted
	created := domainsGateway.CreateEmailForward(ctx, session, "ggalmazor.com", "hola", "guille@sendia.org")
	if created.Failed() || created.Value().From != "hola@ggalmazor.com" {
		test.Fatalf("create forward: %+v / %v", created.Value(), created.Error())
	}
	listed := domainsGateway.EmailForwards(ctx, session, "ggalmazor.com")
	if listed.Failed() || len(listed.Value()) != 1 || listed.Value()[0].To != "guille@sendia.org" {
		test.Fatalf("list forwards: %+v / %v", listed.Value(), listed.Error())
	}
	if outcome := domainsGateway.DeleteEmailForward(ctx, session, "ggalmazor.com", created.Value().ID); outcome.Failed() {
		test.Fatalf("delete forward: %v", outcome.Error())
	}

	// And auto-renew is enabled through the registrar
	if outcome := registrarGateway.SetAutoRenewal(ctx, session, "ggalmazor.com", true); outcome.Failed() {
		test.Fatalf("enabling auto-renew: %v", outcome.Error())
	}

	// Then the portfolio reflects the new renewal mode
	portfolio := domainsGateway.Domains(ctx, session)
	if portfolio.Failed() || len(portfolio.Value()) != 1 || !portfolio.Value()[0].AutoRenew {
		test.Fatalf("portfolio must show auto-renew on: %+v / %v", portfolio.Value(), portfolio.Error())
	}

	// And WHOIS privacy, the transfer lock, delegation, and a renewal all
	// round trip
	if outcome := registrarGateway.SetWhoisPrivacy(ctx, session, "ggalmazor.com", true); outcome.Failed() {
		test.Fatalf("whois privacy on: %v", outcome.Error())
	}
	if privacy := registrarGateway.WhoisPrivacy(ctx, session, "ggalmazor.com"); privacy.Failed() || !privacy.Value() {
		test.Fatalf("whois privacy must read enabled: %v", privacy.Error())
	}
	if outcome := registrarGateway.SetTransferLock(ctx, session, "ggalmazor.com", true); outcome.Failed() {
		test.Fatalf("transfer lock on: %v", outcome.Error())
	}
	if lock := registrarGateway.TransferLock(ctx, session, "ggalmazor.com"); lock.Failed() || !lock.Value() {
		test.Fatalf("transfer lock must read enabled: %v", lock.Error())
	}
	custom := []string{"ns1.example.net", "ns2.example.net"}
	if outcome := registrarGateway.SetDelegation(ctx, session, "ggalmazor.com", custom); outcome.Failed() {
		test.Fatalf("set delegation: %v", outcome.Error())
	}
	delegation := registrarGateway.Delegation(ctx, session, "ggalmazor.com")
	if delegation.Failed() || len(delegation.Value()) != 2 || delegation.Value()[0] != "ns1.example.net" {
		test.Fatalf("delegation: %+v / %v", delegation.Value(), delegation.Error())
	}
	renewal := registrarGateway.Renew(ctx, session, "ggalmazor.com")
	if renewal.Failed() || renewal.Value().ID == 0 {
		test.Fatalf("renewal: %+v / %v", renewal.Value(), renewal.Error())
	}

	// And a transfer in advances to completed and joins the portfolio
	transfer := registrarGateway.Transfer(ctx, session, "incoming.net", 1, "AUTH-123", nil)
	if transfer.Failed() || transfer.Value().Terminal() {
		test.Fatalf("transfer must accept in-flight: %+v / %v", transfer.Value(), transfer.Error())
	}
	if first := registrarGateway.TransferState(ctx, session, "incoming.net", transfer.Value().ID); first.Failed() || first.Value().Terminal() {
		test.Fatalf("first transfer poll must be in flight: %+v", first.Value())
	}
	second := registrarGateway.TransferState(ctx, session, "incoming.net", transfer.Value().ID)
	if second.Failed() || second.Value().State != registrar.TransferCompleted {
		test.Fatalf("second transfer poll: %+v / %v", second.Value(), second.Error())
	}

	// And transfer-out authorization and a restore both accept
	if outcome := registrarGateway.AuthorizeTransferOut(ctx, session, "ggalmazor.com"); outcome.Failed() {
		test.Fatalf("authorize transfer out: %v", outcome.Error())
	}
	if restore := registrarGateway.Restore(ctx, session, "ggalmazor.com"); restore.Failed() || restore.Value().ID == 0 {
		test.Fatalf("restore: %+v / %v", restore.Value(), restore.Error())
	}
}

func TestInvalidEmailForwardsNeverReachTheGateway(test *testing.T) {
	component := domains.NewComponent(brokenGateway{}, nil)

	var invalid domains.InvalidForwardError
	empty := component.CreateEmailForward(context.Background(), identity.Session{}, "x.com", " ", "a@b.c")
	if !errors.As(empty.Error(), &invalid) {
		test.Fatalf("empty alias must fail typed, got %v", empty.Error())
	}
	notEmail := component.CreateEmailForward(context.Background(), identity.Session{}, "x.com", "hola", "not-an-email")
	if !errors.As(notEmail.Error(), &invalid) {
		test.Fatalf("bad destination must fail typed, got %v", notEmail.Error())
	}
}

// brokenGateway fails the test if any method is reached.
type brokenGateway struct{ domains.Gateway }

func TestDSRecordsTemplatesUpdateVanityAndRecordDistributionRoundTrip(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client(), telemetry.NewMetrics())
	domainsComponent := domains.NewComponent(NewDomainsGateway(client), NewRegistrarGateway(client))
	registrarGateway := NewRegistrarGateway(client)
	zonesGateway := NewZonesGateway(client)
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()
	fake.AddDomain("ggalmazor.com", "registered")
	recordIDs := fake.AddZone("ggalmazor.com", Seed("www", "A", "1.2.3.4", 3600))

	// DS records: validation, then the lifecycle
	if outcome := domainsComponent.CreateDSRecord(ctx, session, "ggalmazor.com",
		domains.DSRecord{KeyTag: "12345", Algorithm: "13", DigestType: "2", Digest: "short"}); !outcome.Failed() {
		test.Fatal("a short digest must fail validation")
	}
	created := domainsComponent.CreateDSRecord(ctx, session, "ggalmazor.com", domains.DSRecord{
		KeyTag: "12345", Algorithm: "13", DigestType: "2",
		Digest: "49FD46E6C4B45C55D4AC49FD46E6C4B45C55D4AC",
	})
	if created.Failed() || created.Value().ID == 0 {
		test.Fatalf("create ds record: %+v / %v", created.Value(), created.Error())
	}
	listed := domainsComponent.DSRecords(ctx, session, "ggalmazor.com")
	if listed.Failed() || len(listed.Value()) != 1 {
		test.Fatalf("list ds records: %+v / %v", listed.Value(), listed.Error())
	}
	if deleted := domainsComponent.DeleteDSRecord(ctx, session, "ggalmazor.com", created.Value().ID); deleted.Failed() {
		test.Fatalf("delete ds record: %v", deleted.Error())
	}

	// Vanity name servers toggle both ways
	if outcome := registrarGateway.SetVanityNameServers(ctx, session, "ggalmazor.com", true); outcome.Failed() {
		test.Fatalf("enable vanity: %v", outcome.Error())
	}
	if outcome := registrarGateway.SetVanityNameServers(ctx, session, "ggalmazor.com", false); outcome.Failed() {
		test.Fatalf("disable vanity: %v", outcome.Error())
	}

	// Record-level distribution
	if outcome := zonesGateway.RecordDistribution(ctx, session, "ggalmazor.com", recordIDs[0]); outcome.Failed() || !outcome.Value() {
		test.Fatalf("record distribution: %v", outcome.Error())
	}
}

func TestAHostedDomainIsAddedAndRemovedWithItsZone(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client(), telemetry.NewMetrics())
	component := domains.NewComponent(NewDomainsGateway(client), NewRegistrarGateway(client))
	zonesGateway := NewZonesGateway(client)
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()

	added := component.AddDomain(ctx, session, "  Hosted.ORG ")
	if added.Failed() || added.Value().Name != "hosted.org" {
		test.Fatalf("add: %+v / %v", added.Value(), added.Error())
	}
	if invalid := component.AddDomain(ctx, session, "no-dot"); !invalid.Failed() {
		test.Fatal("a dotless name must be refused before the API call")
	}
	listed := component.Domains(ctx, session)
	if listed.Failed() || len(listed.Value()) != 1 {
		test.Fatalf("domains after add: %+v / %v", listed.Value(), listed.Error())
	}
	zones := zonesGateway.Zones(ctx, session)
	if zones.Failed() || len(zones.Value()) != 1 || zones.Value()[0].Name != "hosted.org" {
		test.Fatalf("the zone must appear with the domain: %+v / %v", zones.Value(), zones.Error())
	}

	if deleted := component.DeleteDomain(ctx, session, "hosted.org"); deleted.Failed() {
		test.Fatalf("delete: %v", deleted.Error())
	}
	// GetFresh path: the domain list is cached for 45s but the delete
	// invalidates writes-through, so a plain read suffices here.
	after := component.Domains(ctx, session)
	if after.Failed() || len(after.Value()) != 0 {
		test.Fatalf("domains after delete: %+v / %v", after.Value(), after.Error())
	}
	if missing := component.DeleteDomain(ctx, session, "hosted.org"); !missing.Failed() {
		test.Fatal("deleting a missing domain must fail")
	}
}

// InspectDomain degrades per field: a setting the account never touched
// (transfer lock answers 404) lands in Partial while the rest render.
func TestInspectDomainDegradesPerFieldInsteadOfBlanking(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client(), telemetry.NewMetrics())
	component := domains.NewComponent(NewDomainsGateway(client), NewRegistrarGateway(client))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()
	// A zone with no domain record: the whois read (via the domain
	// object) answers 404 while the zone-scoped fields load fine.
	fake.AddZone("dnsonly.com")

	settings := component.InspectDomain(ctx, session, "dnsonly.com")
	if len(settings.Partial) == 0 {
		test.Fatal("the failing whois read must degrade into Partial")
	}
	if len(settings.Delegation) == 0 {
		test.Fatal("fields that load must survive a sibling's failure")
	}
}
