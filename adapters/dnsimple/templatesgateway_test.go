package dnsimple

import (
	"context"
	"net/http/httptest"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/core/templates"
	"dnsshimple/core/zones"
)

func TestTemplatesRoundTripAndApplyToAZone(test *testing.T) {
	// Given the templates Component over the real gateway and the fake
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := httptest.NewServer(fake)
	test.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client(), telemetry.NewMetrics())
	component := templates.NewComponent(NewTemplatesGateway(client), telemetry.NewMetrics())
	zonesGateway := NewZonesGateway(client)
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()
	fake.AddZone("ggalmazor.com")

	// When a template with two records is built and applied to the zone
	created := component.CreateTemplate(ctx, session, templates.Template{
		Name: "Web basics", SID: "web-basics", Description: "A + www",
	})
	if created.Failed() || created.Value().ID == 0 {
		test.Fatalf("create template: %+v / %v", created.Value(), created.Error())
	}
	templateID := created.Value().ID
	for _, draft := range []zones.RecordDraft{
		{Name: "", Type: "A", Content: "1.2.3.4", TTL: 3600},
		{Name: "www", Type: "CNAME", Content: "ggalmazor.com", TTL: 3600},
	} {
		if outcome := component.CreateTemplateRecord(ctx, session, templateID, draft); outcome.Failed() {
			test.Fatalf("template record: %v", outcome.Error())
		}
	}
	records := component.TemplateRecords(ctx, session, templateID)
	if records.Failed() || len(records.Value()) != 2 {
		test.Fatalf("template records: %+v / %v", records.Value(), records.Error())
	}
	if applied := component.ApplyTemplate(ctx, session, "ggalmazor.com", templateID); applied.Failed() {
		test.Fatalf("apply: %v", applied.Error())
	}

	// Then the zone carries the template's records
	zoneRecords := zonesGateway.Records(ctx, session, "ggalmazor.com")
	if zoneRecords.Failed() || len(zoneRecords.Value()) != 2 {
		test.Fatalf("zone after apply: %+v / %v", zoneRecords.Value(), zoneRecords.Error())
	}

	// And an invalid template record never reaches the API
	invalid := component.CreateTemplateRecord(ctx, session, templateID,
		zones.RecordDraft{Type: "WKS", Content: "x", TTL: 3600})
	if !invalid.Failed() {
		test.Fatal("an unsupported type must fail validation")
	}

	// And deleting the template removes it from the list
	if outcome := component.DeleteTemplate(ctx, session, templateID); outcome.Failed() {
		test.Fatalf("delete template: %v", outcome.Error())
	}
	remaining := component.Templates(ctx, session)
	if remaining.Failed() || len(remaining.Value()) != 0 {
		test.Fatalf("after delete: %+v / %v", remaining.Value(), remaining.Error())
	}
}
