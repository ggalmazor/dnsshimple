// Package contacts is the Component for account contacts (DESIGN.md §3.1,
// M7): the registrant data behind registrations, transfers, and registrant
// changes.
package contacts

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type Contact struct {
	ID            int64
	FirstName     string
	LastName      string
	Email         string
	Phone         string
	Address       string
	City          string
	StateProvince string
	PostalCode    string
	Country       string
}

// Draft is a Contact without an identity: what create and update carry.
type Draft struct {
	FirstName     string
	LastName      string
	Email         string
	Phone         string
	Address       string
	City          string
	StateProvince string
	PostalCode    string
	Country       string
}

func (contact Contact) Label() string {
	return strings.TrimSpace(contact.FirstName+" "+contact.LastName) + " <" + contact.Email + ">"
}

type InvalidContactError struct {
	Reason string
}

func (invalid InvalidContactError) Error() string {
	return "contacts: invalid contact: " + invalid.Reason
}

var (
	emailPattern   = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phonePattern   = regexp.MustCompile(`^\+[0-9][0-9 .-]{5,}$`)
	countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
)

// ValidateDraft enforces what registries require of registrant data, so a
// half-filled form fails here instead of at the API.
func ValidateDraft(draft Draft) error {
	required := map[string]string{
		"first name": draft.FirstName, "last name": draft.LastName,
		"address": draft.Address, "city": draft.City,
		"state/province": draft.StateProvince, "postal code": draft.PostalCode,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return InvalidContactError{Reason: "the " + field + " must not be empty"}
		}
	}
	if !emailPattern.MatchString(draft.Email) {
		return InvalidContactError{Reason: fmt.Sprintf("%q is not an email address", draft.Email)}
	}
	if !phonePattern.MatchString(draft.Phone) {
		return InvalidContactError{Reason: "the phone must be international format, e.g. +34 600 000 000"}
	}
	if !countryPattern.MatchString(draft.Country) {
		return InvalidContactError{Reason: "the country must be a 2-letter code, e.g. ES"}
	}
	return nil
}

type Gateway interface {
	Contacts(ctx context.Context, session identity.Session) kit.Outcome[[]Contact]
	CreateContact(ctx context.Context, session identity.Session, draft Draft) kit.Outcome[Contact]
	UpdateContact(ctx context.Context, session identity.Session, contactID int64, draft Draft) kit.Outcome[Contact]
	DeleteContact(ctx context.Context, session identity.Session, contactID int64) kit.Outcome[kit.Nothing]
}

type Component struct {
	gateway Gateway
}

func NewComponent(gateway Gateway) *Component {
	return &Component{gateway: gateway}
}

func (component *Component) Contacts(ctx context.Context, session identity.Session) kit.Outcome[[]Contact] {
	return component.gateway.Contacts(ctx, session)
}

func (component *Component) CreateContact(ctx context.Context, session identity.Session, draft Draft) kit.Outcome[Contact] {
	if err := ValidateDraft(draft); err != nil {
		return kit.Failure[Contact](err)
	}
	created := component.gateway.CreateContact(ctx, session, draft)
	if !created.Failed() {
		slog.InfoContext(ctx, "contact created", "contact_id", created.Value().ID)
	}
	return created
}

func (component *Component) UpdateContact(ctx context.Context, session identity.Session, contactID int64, draft Draft) kit.Outcome[Contact] {
	if err := ValidateDraft(draft); err != nil {
		return kit.Failure[Contact](err)
	}
	return component.gateway.UpdateContact(ctx, session, contactID, draft)
}

// DeleteContact passes DNSimple's refusal through untouched: a contact in
// use by a domain cannot be deleted, and DNSimple's message says which.
func (component *Component) DeleteContact(ctx context.Context, session identity.Session, contactID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.DeleteContact(ctx, session, contactID)
}
