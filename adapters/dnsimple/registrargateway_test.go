package dnsimple

import (
	"context"
	"errors"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/registrar"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
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
	accepted := registrarGateway.Register(ctx, session, "dnsshimple.com", contactID, nil)
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

	accepted := gateway.Transfer(ctx, session, "incoming.com", contactID, "auth-code", nil)
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

func TestVanityDelegationRequiresTheFeatureAndRoundTrips(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	gateway := NewRegistrarGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics()))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()
	fake.AddDomain("ggalmazor.com", "registered")
	vanityServers := []string{"ns1.ggalmazor.com", "ns2.ggalmazor.com"}

	// Production answers 412 until vanity name servers are enabled.
	if premature := gateway.DelegateToVanity(ctx, session, "ggalmazor.com", vanityServers); !premature.Failed() {
		test.Fatal("delegating without the feature enabled must fail")
	}
	if enabled := gateway.SetVanityNameServers(ctx, session, "ggalmazor.com", true); enabled.Failed() {
		test.Fatalf("enable vanity: %v", enabled.Error())
	}
	if delegated := gateway.DelegateToVanity(ctx, session, "ggalmazor.com", vanityServers); delegated.Failed() {
		test.Fatalf("delegate: %v", delegated.Error())
	}
	delegation := gateway.Delegation(ctx, session, "ggalmazor.com")
	if delegation.Failed() || len(delegation.Value()) != 2 || delegation.Value()[0] != "ns1.ggalmazor.com" {
		test.Fatalf("delegation after vanity: %+v / %v", delegation.Value(), delegation.Error())
	}
	if dedelegated := gateway.DedelegateFromVanity(ctx, session, "ggalmazor.com"); dedelegated.Failed() {
		test.Fatalf("dedelegate: %v", dedelegated.Error())
	}
}

func TestUKRegistrationsDemandTheirExtendedAttributes(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	gateway := NewRegistrarGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics()))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	contactID := fake.AddContact("Guillermo", "G", "guille@sendia.org")
	ctx := context.Background()

	if bare := gateway.Register(ctx, session, "felix.uk", contactID, nil); !bare.Failed() {
		test.Fatal("a .uk registration without uk_legal_type must be refused")
	}
	attributes := map[string]string{"uk_legal_type": "IND"}
	if accepted := gateway.Register(ctx, session, "felix.uk", contactID, attributes); accepted.Failed() {
		test.Fatalf("register with attributes: %v", accepted.Error())
	}
	if bare := gateway.Transfer(ctx, session, "incoming.uk", contactID, "auth", nil); !bare.Failed() {
		test.Fatal("a .uk transfer without uk_legal_type must be refused")
	}
	if accepted := gateway.Transfer(ctx, session, "incoming.uk", contactID, "auth", attributes); accepted.Failed() {
		test.Fatalf("transfer with attributes: %v", accepted.Error())
	}
}

// The registry-demand refusal at the Registrar port: the BTX refuses with
// the form definition before spending the write, and accepts once the
// attributes ride along (DESIGN §3: no business logic in the TUI).
func TestRegisteringADemandingTLDRefusesTypedWithTheFormDefinition(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	component := registrar.NewComponent(
		NewRegistrarGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics())),
		recordingQueue{}, telemetry.NewMetrics())
	fake.AddContact("Guillermo", "G", "guille@sendia.org")
	session := identity.Session{Tenant: identity.Tenant{ID: 1, AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()

	bare := component.RegisterDomain(ctx, session, "felix.uk", nil)
	var needs registrar.NeedsExtendedAttributesError
	if !errors.As(bare.Error(), &needs) || len(needs.Demands) != 1 || needs.Demands[0].Name != "uk_legal_type" {
		test.Fatalf("must refuse with the form definition: %v", bare.Error())
	}
	supplied := component.RegisterDomain(ctx, session, "felix.uk", map[string]string{"uk_legal_type": "IND"})
	if supplied.Failed() {
		test.Fatalf("register with attributes: %v", supplied.Error())
	}
}

type recordingQueue struct{}

func (recordingQueue) EnqueueRegistrationPoll(context.Context, int64, string, int64) kit.Outcome[kit.Nothing] {
	return kit.Done()
}

func (recordingQueue) EnqueueCertificatePoll(context.Context, int64, string, int64) kit.Outcome[kit.Nothing] {
	return kit.Done()
}

func (recordingQueue) EnqueueTransferPoll(context.Context, int64, string, int64) kit.Outcome[kit.Nothing] {
	return kit.Done()
}

func (recordingQueue) EnqueueRegistrantChangePoll(context.Context, int64, int64) kit.Outcome[kit.Nothing] {
	return kit.Done()
}
