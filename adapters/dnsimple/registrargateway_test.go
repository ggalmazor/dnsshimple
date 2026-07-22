package dnsimple

import (
	"context"
	"net/http/httptest"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/registrar"
	"dnsshimple/core/telemetry"
)

// Grey-box across the registrar and domains gateways: drive the ports against
// the fake's registration state machine and assert on what the account ends
// up holding.
func TestARegistrationAdvancesToRegisteredAndLandsInTheDomainList(test *testing.T) {
	// Given an account with a contact and no domains
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := httptest.NewServer(fake)
	test.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client(), telemetry.NewMetrics())
	registrarGateway := NewRegistrarGateway(client)
	domainsGateway := NewDomainsGateway(client)
	contactID := fake.AddContact("Guillermo", "G", "guille@sendia.org")
	session := identity.Session{
		Tenant:      identity.Tenant{ID: 1, AccountID: 2416},
		AccessToken: "fake-access-token-2416",
	}
	ctx := context.Background()

	// When the domain is checked, registered, and polled to completion
	availability := registrarGateway.CheckAvailability(ctx, session, "dnsshimple.com")
	if availability.Failed() || !availability.Value().Available {
		test.Fatalf("check: %+v / %v", availability.Value(), availability.Error())
	}
	contacts := registrarGateway.Contacts(ctx, session)
	if contacts.Failed() || len(contacts.Value()) != 1 || contacts.Value()[0].ID != contactID {
		test.Fatalf("contacts: %+v / %v", contacts.Value(), contacts.Error())
	}
	accepted := registrarGateway.Register(ctx, session, "dnsshimple.com", contactID)
	if accepted.Failed() || accepted.Value().Terminal() {
		test.Fatalf("register must accept as in-flight: %+v / %v", accepted.Value(), accepted.Error())
	}

	firstPoll := registrarGateway.RegistrationState(ctx, session, "dnsshimple.com", accepted.Value().ID)
	if firstPoll.Failed() || firstPoll.Value().Terminal() {
		test.Fatalf("first poll must still be in flight: %+v", firstPoll.Value())
	}
	secondPoll := registrarGateway.RegistrationState(ctx, session, "dnsshimple.com", accepted.Value().ID)
	if secondPoll.Failed() || secondPoll.Value().State != registrar.StateRegistered {
		test.Fatalf("second poll must be registered: %+v / %v", secondPoll.Value(), secondPoll.Error())
	}

	// Then the domain shows up in the portfolio
	// (the write invalidated nothing here: polls are GETs on a fresh path)
	listed := domainsGateway.Domains(ctx, session)
	if listed.Failed() || len(listed.Value()) != 1 || listed.Value()[0].Name != "dnsshimple.com" {
		test.Fatalf("domains: %+v / %v", listed.Value(), listed.Error())
	}
}
