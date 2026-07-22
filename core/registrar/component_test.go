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

func TestDelegationValidatesBeforeSpendingACall(test *testing.T) {
	gateway := &fakeGateway{}
	component := NewComponent(gateway, &fakeQueue{}, telemetry.NewMetrics())
	ctx := context.Background()

	var invalid InvalidDomainError
	tooFew := component.SetDelegation(ctx, identity.Session{}, "x.com", []string{"ns1.dnsimple.com"})
	if !errors.As(tooFew.Error(), &invalid) {
		test.Fatalf("one name server must fail typed, got %v", tooFew.Error())
	}
	badHost := component.SetDelegation(ctx, identity.Session{}, "x.com", []string{"ns1.dnsimple.com", "not a host!"})
	if !errors.As(badHost.Error(), &invalid) {
		test.Fatalf("a bad hostname must fail typed, got %v", badHost.Error())
	}
	if gateway.calls != 0 {
		test.Fatalf("the gateway was called %d times for invalid delegations", gateway.calls)
	}

	if valid := component.SetDelegation(ctx, identity.Session{}, "x.com",
		[]string{"ns1.example.net", "ns2.example.net"}); valid.Failed() {
		test.Fatalf("a valid delegation must pass: %v", valid.Error())
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

func (gateway *fakeGateway) WhoisPrivacy(context.Context, identity.Session, string) kit.Outcome[bool] {
	gateway.calls++
	return kit.Success(false)
}

func (gateway *fakeGateway) SetWhoisPrivacy(context.Context, identity.Session, string, bool) kit.Outcome[kit.Nothing] {
	gateway.calls++
	return kit.Done()
}

func (gateway *fakeGateway) TransferLock(context.Context, identity.Session, string) kit.Outcome[bool] {
	gateway.calls++
	return kit.Success(false)
}

func (gateway *fakeGateway) SetTransferLock(context.Context, identity.Session, string, bool) kit.Outcome[kit.Nothing] {
	gateway.calls++
	return kit.Done()
}

func (gateway *fakeGateway) Delegation(context.Context, identity.Session, string) kit.Outcome[[]string] {
	gateway.calls++
	return kit.Success([]string{"ns1.dnsimple.com", "ns2.dnsimple.com"})
}

func (gateway *fakeGateway) SetDelegation(context.Context, identity.Session, string, []string) kit.Outcome[kit.Nothing] {
	gateway.calls++
	return kit.Done()
}

func (gateway *fakeGateway) Renew(context.Context, identity.Session, string) kit.Outcome[Renewal] {
	gateway.calls++
	return kit.Success(Renewal{ID: 77, State: "new"})
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
