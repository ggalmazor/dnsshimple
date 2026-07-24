package dnsimple

import (
	"context"
	"testing"

	"dnsshimple/core/domains"
	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/core/templates"
	"dnsshimple/core/zones"
)

// One walk across every single-item show operation, each answered by the
// fake under the contract check.
func TestEverySingleItemShowOperationRoundTrips(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client(), telemetry.NewMetrics())
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()

	recordIDs := fake.AddZone("example.com", fakeSeed{Type: "A", Name: "www", Content: "1.2.3.4", TTL: 3600})
	contactID := fake.AddContact("Guillermo", "G", "guille@sendia.org")

	if listed := NewAccountsGateway(client).Accounts(ctx, "token"); listed.Failed() ||
		len(listed.Value()) != 1 || listed.Value()[0].Email != "guille@sendia.org" {
		test.Fatalf("accounts: %+v / %v", listed.Value(), listed.Error())
	}

	domainsGateway := NewDomainsGateway(client)
	research := domainsGateway.ResearchDomainStatus(ctx, session, "fresh-name.com")
	if research.Failed() || research.Value().Availability != "available" {
		test.Fatalf("research: %+v / %v", research.Value(), research.Error())
	}

	if contact := NewContactsGateway(client).Contact(ctx, session, contactID); contact.Failed() {
		test.Fatalf("contact: %v", contact.Error())
	}

	zonesGateway := NewZonesGateway(client)
	if zone := zonesGateway.Zone(ctx, session, "example.com"); zone.Failed() || zone.Value().Name != "example.com" {
		test.Fatalf("zone: %+v / %v", zone.Value(), zone.Error())
	}
	if record := zonesGateway.Record(ctx, session, "example.com", recordIDs[0]); record.Failed() ||
		record.Value().Content != "1.2.3.4" {
		test.Fatalf("zone record: %+v / %v", record.Value(), record.Error())
	}

	if tld := NewRegistrarGateway(client).TLD(ctx, session, "dev"); tld.Failed() || tld.Value().Name != "dev" {
		test.Fatalf("tld: %+v / %v", tld.Value(), tld.Error())
	}

	templatesGateway := NewTemplatesGateway(client)
	created := templatesGateway.CreateTemplate(ctx, session, templates.Template{Name: "Base", SID: "base"})
	if created.Failed() {
		test.Fatalf("create template: %v", created.Error())
	}
	if template := templatesGateway.Template(ctx, session, created.Value().ID); template.Failed() {
		test.Fatalf("template: %v", template.Error())
	}
	templateRecord := templatesGateway.CreateTemplateRecord(ctx, session, created.Value().ID,
		zones.RecordDraft{Type: "A", Name: "www", Content: "1.2.3.4", TTL: 3600})
	if templateRecord.Failed() {
		test.Fatalf("create template record: %v", templateRecord.Error())
	}
	if shown := templatesGateway.TemplateRecord(ctx, session, created.Value().ID, templateRecord.Value().ID); shown.Failed() {
		test.Fatalf("template record: %v", shown.Error())
	}

	webhooksGateway := NewWebhooksGateway(client)
	hook := webhooksGateway.CreateWebhook(ctx, session, "https://sendia.org/hook")
	if hook.Failed() {
		test.Fatalf("create webhook: %v", hook.Error())
	}
	if shown := webhooksGateway.Webhook(ctx, session, hook.Value().ID); shown.Failed() {
		test.Fatalf("webhook: %v", shown.Error())
	}

	secondaryGateway := NewSecondaryDNSGateway(client)
	primary := secondaryGateway.CreatePrimary(ctx, session, "primary-1", "1.1.1.1", 53)
	if primary.Failed() {
		test.Fatalf("create primary: %v", primary.Error())
	}
	if shown := secondaryGateway.Primary(ctx, session, primary.Value().ID); shown.Failed() {
		test.Fatalf("primary: %v", shown.Error())
	}

	servicesGateway := NewServicesGateway(client)
	available := servicesGateway.AvailableServices(ctx, session)
	if available.Failed() || len(available.Value()) == 0 {
		test.Fatalf("available services: %v", available.Error())
	}
	if shown := servicesGateway.Service(ctx, session, available.Value()[0].ID); shown.Failed() {
		test.Fatalf("service: %v", shown.Error())
	}

	fake.AddDomain("shown.com", "registered")
	dsCreated := domainsGateway.CreateDSRecord(ctx, session, "shown.com", domains.DSRecord{
		KeyTag: "12345", Algorithm: "13", DigestType: "2",
		Digest: "1c02e2e2a1f2c34d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d",
	})
	if dsCreated.Failed() {
		test.Fatalf("create ds: %v", dsCreated.Error())
	}
	if shown := domainsGateway.DSRecord(ctx, session, "shown.com", dsCreated.Value().ID); shown.Failed() {
		test.Fatalf("ds record: %v", shown.Error())
	}
	forward := domainsGateway.CreateEmailForward(ctx, session, "shown.com", "hi", "guille@sendia.org")
	if forward.Failed() {
		test.Fatalf("create forward: %v", forward.Error())
	}
	if shown := domainsGateway.EmailForward(ctx, session, "shown.com", forward.Value().ID); shown.Failed() {
		test.Fatalf("email forward: %v", shown.Error())
	}

	registrarGateway := NewRegistrarGateway(client)
	renewal := registrarGateway.Renew(ctx, session, "shown.com")
	if renewal.Failed() {
		test.Fatalf("renew: %v", renewal.Error())
	}
	if shown := registrarGateway.RenewalState(ctx, session, "shown.com", renewal.Value().ID); shown.Failed() {
		test.Fatalf("renewal state: %v", shown.Error())
	}
	restore := registrarGateway.Restore(ctx, session, "shown.com")
	if restore.Failed() {
		test.Fatalf("restore: %v", restore.Error())
	}
	if shown := registrarGateway.RestoreState(ctx, session, "shown.com", restore.Value().ID); shown.Failed() {
		test.Fatalf("restore state: %v", shown.Error())
	}
}
