package dnsimple

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"dnsshimple/core/domains"
	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
)

func TestDomainSettingsRoundTripThroughDNSimple(test *testing.T) {
	// Given a registered domain
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := httptest.NewServer(fake)
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
}

func TestInvalidEmailForwardsNeverReachTheGateway(test *testing.T) {
	component := domains.NewComponent(brokenGateway{})

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
