package dnsimple

import (
	"context"
	"errors"
	"testing"

	"dnsshimple/core/contacts"
	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
)

func TestContactsRoundTripThroughDNSimple(test *testing.T) {
	// Given the contacts Component over the real gateway and the fake
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	component := contacts.NewComponent(NewContactsGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics())))
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()
	draft := contacts.Draft{
		FirstName: "Guillermo", LastName: "G", Email: "guille@sendia.org",
		Phone: "+34 600 000 000", Address: "Calle Mayor 1", City: "Madrid",
		StateProvince: "Madrid", PostalCode: "28001", Country: "ES",
	}

	// When a contact is created, updated, listed, and deleted
	created := component.CreateContact(ctx, session, draft)
	if created.Failed() || created.Value().ID == 0 {
		test.Fatalf("create: %+v / %v", created.Value(), created.Error())
	}
	draft.City = "Sevilla"
	updated := component.UpdateContact(ctx, session, created.Value().ID, draft)
	if updated.Failed() || updated.Value().City != "Sevilla" {
		test.Fatalf("update: %+v / %v", updated.Value(), updated.Error())
	}
	listed := component.Contacts(ctx, session)
	if listed.Failed() || len(listed.Value()) != 1 || listed.Value()[0].City != "Sevilla" {
		test.Fatalf("list: %+v / %v", listed.Value(), listed.Error())
	}
	if deleted := component.DeleteContact(ctx, session, created.Value().ID); deleted.Failed() {
		test.Fatalf("delete: %v", deleted.Error())
	}

	// Then the account has no contacts left
	remaining := component.Contacts(ctx, session)
	if remaining.Failed() || len(remaining.Value()) != 0 {
		test.Fatalf("after delete: %+v / %v", remaining.Value(), remaining.Error())
	}
}

func TestInvalidContactDraftsFailTypedBeforeTheAPI(test *testing.T) {
	component := contacts.NewComponent(nil) // the gateway must never be reached
	session := identity.Session{}
	ctx := context.Background()
	valid := contacts.Draft{
		FirstName: "G", LastName: "G", Email: "g@x.com", Phone: "+34 600 000 000",
		Address: "x", City: "x", StateProvince: "x", PostalCode: "x", Country: "ES",
	}

	broken := map[string]func(contacts.Draft) contacts.Draft{
		"empty first name": func(draft contacts.Draft) contacts.Draft { draft.FirstName = " "; return draft },
		"bad email":        func(draft contacts.Draft) contacts.Draft { draft.Email = "nope"; return draft },
		"local phone":      func(draft contacts.Draft) contacts.Draft { draft.Phone = "600000000"; return draft },
		"bad country":      func(draft contacts.Draft) contacts.Draft { draft.Country = "Spain"; return draft },
	}
	for name, mutate := range broken {
		var invalid contacts.InvalidContactError
		if outcome := component.CreateContact(ctx, session, mutate(valid)); !errors.As(outcome.Error(), &invalid) {
			test.Fatalf("%s must fail typed, got %v", name, outcome.Error())
		}
	}
}
