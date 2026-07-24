package dnsimple

import (
	"context"
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
	server := newContractServer(test, fake)
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

func TestARegistrantChangeChecksCreatesAndAdvances(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	gateway := NewRegistrarGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics()))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()

	checked := gateway.CheckRegistrantChange(ctx, session, 10, 7)
	if checked.Failed() || checked.Value().RequiresExtendedAttributes {
		test.Fatalf("check: %+v / %v", checked.Value(), checked.Error())
	}
	created := gateway.CreateRegistrantChange(ctx, session, 10, 7)
	if created.Failed() || created.Value().Terminal() {
		test.Fatalf("create must accept in-flight: %+v / %v", created.Value(), created.Error())
	}
	if first := gateway.RegistrantChangeState(ctx, session, created.Value().ID); first.Failed() || first.Value().Terminal() {
		test.Fatalf("first poll must be in flight: %+v", first.Value())
	}
	second := gateway.RegistrantChangeState(ctx, session, created.Value().ID)
	if second.Failed() || second.Value().State != registrar.RegistrantChangeCompleted {
		test.Fatalf("second poll: %+v / %v", second.Value(), second.Error())
	}
}

func TestAPendingRegistrantChangeIsListedAndCancellable(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	gateway := NewRegistrarGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics()))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()

	created := gateway.CreateRegistrantChange(ctx, session, 10, 7)
	if created.Failed() {
		test.Fatalf("create: %v", created.Error())
	}
	listed := gateway.RegistrantChanges(ctx, session)
	if listed.Failed() || len(listed.Value()) != 1 || listed.Value()[0].ID != created.Value().ID {
		test.Fatalf("list must hold the pending change: %+v / %v", listed.Value(), listed.Error())
	}
	if cancelled := gateway.CancelRegistrantChange(ctx, session, created.Value().ID); cancelled.Failed() {
		test.Fatalf("cancel: %v", cancelled.Error())
	}
	// A poll after the cancel must observe the terminal cancelled state, not
	// resurrect the change into completion.
	after := gateway.RegistrantChangeState(ctx, session, created.Value().ID)
	if after.Failed() || after.Value().State != registrar.RegistrantChangeCancelled {
		test.Fatalf("state after cancel: %+v / %v", after.Value(), after.Error())
	}
	if again := gateway.CancelRegistrantChange(ctx, session, created.Value().ID); !again.Failed() {
		test.Fatal("cancelling a terminal change must fail")
	}
}

func TestAnInFlightTransferIsCancellable(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	gateway := NewRegistrarGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics()))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	contactID := fake.AddContact("Guillermo", "G", "guille@sendia.org")
	ctx := context.Background()

	accepted := gateway.Transfer(ctx, session, "incoming.com", contactID, "auth-code")
	if accepted.Failed() || accepted.Value().Terminal() {
		test.Fatalf("transfer must accept in-flight: %+v / %v", accepted.Value(), accepted.Error())
	}
	if cancelled := gateway.CancelTransfer(ctx, session, "incoming.com", accepted.Value().ID); cancelled.Failed() {
		test.Fatalf("cancel: %v", cancelled.Error())
	}
	after := gateway.TransferState(ctx, session, "incoming.com", accepted.Value().ID)
	if after.Failed() || after.Value().State != registrar.TransferCancelled {
		test.Fatalf("state after cancel must stay cancelled: %+v / %v", after.Value(), after.Error())
	}
}
