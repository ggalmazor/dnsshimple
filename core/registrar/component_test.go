package registrar

import (
	"context"
	"errors"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

func TestRegisterDomainRunsTheRailwayAndQueuesThePoll(test *testing.T) {
	// Given an available domain and an account with a contact
	gateway := &fakeGateway{available: true, contacts: []Contact{{ID: 7, Label: "Guillermo"}}}
	queue := &fakeQueue{}
	component := NewComponent(gateway, queue, telemetry.NewMetrics())
	session := identity.Session{Tenant: identity.Tenant{ID: 42}}

	// When the domain is registered
	outcome := component.RegisterDomain(context.Background(), session, "dnsshimple.com")

	// Then the registration is accepted with the first contact and the poll
	// is queued for the tenant
	if outcome.Failed() {
		test.Fatalf("RegisterDomain failed: %v", outcome.Error())
	}
	if gateway.registeredWith != 7 {
		test.Fatalf("registered with contact %d, expected 7", gateway.registeredWith)
	}
	if queue.tenantID != 42 || queue.domainName != "dnsshimple.com" || queue.registrationID != outcome.Value().ID {
		test.Fatalf("poll queued wrong: %+v", queue)
	}
}

func TestRegisterDomainFailsTypedWithoutSpendingCalls(test *testing.T) {
	session := identity.Session{Tenant: identity.Tenant{ID: 42}}
	ctx := context.Background()

	// An invalid name never reaches the gateway
	gateway := &fakeGateway{}
	component := NewComponent(gateway, &fakeQueue{}, telemetry.NewMetrics())
	var invalid InvalidDomainError
	if outcome := component.RegisterDomain(ctx, session, "Not A Domain"); !errors.As(outcome.Error(), &invalid) {
		test.Fatalf("expected InvalidDomainError, got %v", outcome.Error())
	}
	if gateway.calls != 0 {
		test.Fatal("the gateway was called for an invalid name")
	}

	// A taken domain stops before contacts and registration
	gateway = &fakeGateway{available: false}
	component = NewComponent(gateway, &fakeQueue{}, telemetry.NewMetrics())
	if outcome := component.RegisterDomain(ctx, session, "taken.com"); !errors.Is(outcome.Error(), ErrDomainTaken) {
		test.Fatalf("expected ErrDomainTaken, got %v", outcome.Error())
	}
	if gateway.calls != 1 {
		test.Fatalf("expected only the availability call, got %d", gateway.calls)
	}

	// No contacts stops before registration, with the actionable error
	gateway = &fakeGateway{available: true}
	component = NewComponent(gateway, &fakeQueue{}, telemetry.NewMetrics())
	if outcome := component.RegisterDomain(ctx, session, "dnsshimple.com"); !errors.Is(outcome.Error(), ErrNoContacts) {
		test.Fatalf("expected ErrNoContacts, got %v", outcome.Error())
	}
	if gateway.registeredWith != 0 {
		test.Fatal("a registration happened without a contact")
	}
}

type fakeGateway struct {
	available      bool
	contacts       []Contact
	calls          int
	registeredWith int64
}

func (gateway *fakeGateway) CheckAvailability(_ context.Context, _ identity.Session, domainName string) kit.Outcome[Availability] {
	gateway.calls++
	return kit.Success(Availability{Domain: domainName, Available: gateway.available})
}

func (gateway *fakeGateway) Contacts(context.Context, identity.Session) kit.Outcome[[]Contact] {
	gateway.calls++
	return kit.Success(gateway.contacts)
}

func (gateway *fakeGateway) Register(_ context.Context, _ identity.Session, _ string, contactID int64) kit.Outcome[Registration] {
	gateway.calls++
	gateway.registeredWith = contactID
	return kit.Success(Registration{ID: 900, State: "registering"})
}

func (gateway *fakeGateway) RegistrationState(context.Context, identity.Session, string, int64) kit.Outcome[Registration] {
	gateway.calls++
	return kit.Success(Registration{ID: 900, State: StateRegistered})
}

func (gateway *fakeGateway) SetAutoRenewal(context.Context, identity.Session, string, bool) kit.Outcome[kit.Nothing] {
	gateway.calls++
	return kit.Done()
}

type fakeQueue struct {
	tenantID       int64
	domainName     string
	registrationID int64
}

func (queue *fakeQueue) EnqueueRegistrationPoll(_ context.Context, tenantID int64, domainName string, registrationID int64) kit.Outcome[kit.Nothing] {
	queue.tenantID, queue.domainName, queue.registrationID = tenantID, domainName, registrationID
	return kit.Done()
}
